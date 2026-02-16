"""Risk manager - main entry point for risk evaluation."""

import logging
from datetime import datetime, timezone
from decimal import Decimal
from typing import TYPE_CHECKING

from hyperhandler.models import Position, TradingSignal
from hyperhandler.models.risk import (
    CircuitBreakerStatus,
    RejectReason,
    RiskDecisionLog,
    RiskLevel,
    RiskMode,
    RiskReject,
    TradeOrder,
    TradeResult,
)
from hyperhandler.models.signal import SignalHorizon
from hyperhandler.risk.calculator import RiskCalculator
from hyperhandler.risk.circuit_breaker import CircuitBreaker
from hyperhandler.risk.config import ATR_SETTINGS, HLConfig, RiskProfile

if TYPE_CHECKING:
    from hyperhandler.client.info import InfoClient
    from hyperhandler.storage import Storage

logger = logging.getLogger(__name__)


class RiskManager:
    """Stateless risk evaluator.

    All data comes through arguments — no internal state.
    Allows testing and backtesting through single interface.
    """

    def __init__(
        self,
        risk_level: RiskLevel = RiskLevel.MEDIUM,
        risk_mode: RiskMode = RiskMode.MANUAL,
        hl_config: HLConfig | None = None,
    ):
        """Initialize RiskManager.

        Args:
            risk_level: Risk tolerance level (LOW/MEDIUM/HIGH).
            risk_mode: MANUAL (validate-only) or MANAGED (full sizing).
            hl_config: Hyperliquid-specific configuration.
        """
        self.risk_level = risk_level
        self.risk_mode = risk_mode
        self.profile = RiskProfile.get(risk_level)
        self.hl_config = hl_config or HLConfig()
        self.calculator = RiskCalculator(self.profile, self.hl_config)
        self.circuit_breaker = CircuitBreaker(self.profile)
        self._last_decision_log: RiskDecisionLog | None = None

    async def evaluate_signal(
        self,
        signal: TradingSignal,
        info_client: "InfoClient",
        address: str,
        trade_history: list[TradeResult] | None = None,
        storage: "Storage | None" = None,
        network: str = "mainnet",
    ) -> TradeOrder | RiskReject:
        """Main entry point. Evaluates signal and returns order or reject.

        Args:
            signal: Trading signal to evaluate.
            info_client: HL info client for market data.
            address: User's wallet address.
            trade_history: Recent closed trades for circuit breaker.
            storage: Optional storage for persisting decision log.
            network: Network name for storage.

        Returns:
            TradeOrder if approved, RiskReject if rejected.
        """
        # Fetch all required data
        account_state = await info_client.get_account_state(address)
        positions = await info_client.get_positions(address)
        asset_meta = await info_client.get_asset_info(signal.pair)
        mark_price = await info_client.get_mid_price(signal.pair)
        funding_rate = await info_client.get_funding_rate(signal.pair)

        # Add asset_id to meta for later use
        asset_id = await info_client.get_asset_index(signal.pair)
        asset_meta["_asset_id"] = asset_id

        # Get candles for ATR (only in MANAGED mode or if no SL in signal)
        candles: list[dict] = []
        if self.risk_mode == RiskMode.MANAGED or signal.stop_loss is None:
            interval = self._get_candle_interval(signal.horizon)
            candles = await info_client.get_candles(signal.pair, interval)

        # Extract account metrics
        margin_summary = account_state.get("marginSummary", {})
        account_value = Decimal(str(margin_summary.get("accountValue", "0")))
        available_balance = Decimal(str(account_state.get("withdrawable", "0")))

        result = self.evaluate_signal_with_data(
            signal=signal,
            account_value=account_value,
            available_balance=available_balance,
            open_positions=positions,
            asset_meta=asset_meta,
            candles=candles,
            funding_rate=funding_rate,
            mark_price=mark_price,
            trade_history=trade_history,
        )

        # Persist decision log if storage provided
        if storage and self._last_decision_log:
            self._last_decision_log.persist(storage, network)

        return result

    def evaluate_signal_with_data(
        self,
        signal: TradingSignal,
        account_value: Decimal,
        available_balance: Decimal,
        open_positions: list[Position],
        asset_meta: dict,
        candles: list[dict],
        funding_rate: Decimal,
        mark_price: Decimal,
        trade_history: list[TradeResult] | None = None,
    ) -> TradeOrder | RiskReject:
        """Pure function version for testing/backtesting.

        All data provided as arguments. Behavior depends on self.risk_mode:
        - MANUAL: validate signal as-is, reject if limits exceeded
        - MANAGED: calculate optimal size/sl/leverage from risk budget
        """
        trade_history = trade_history or []

        # 1. Circuit breaker check (both modes)
        cb_status = self.circuit_breaker.check(trade_history, account_value)
        cb_reject = self.circuit_breaker.get_reject(cb_status)
        if cb_reject:
            self._log_decision(
                signal, None, cb_reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return cb_reject

        # 2. Validate signal vs market (entry price deviation)
        entry_price = signal.entry_price or mark_price
        if signal.entry_price:
            deviation = abs(entry_price - mark_price) / mark_price
            if deviation > self.hl_config.max_entry_deviation:
                reject = RiskReject(
                    reason=RejectReason.STALE_SIGNAL,
                    details=f"Entry deviation {deviation:.1%} > max {self.hl_config.max_entry_deviation:.1%}",
                    suggested_action="wait",
                )
                self._log_decision(
                    signal, None, reject, mark_price, funding_rate,
                    account_value, available_balance, open_positions, cb_status
                )
                return reject

        # 3. Check duplicate position
        for pos in open_positions:
            if pos.coin == signal.pair:
                is_same_side = (
                    (pos.is_long and signal.side.value == "long") or
                    (pos.is_short and signal.side.value == "short")
                )
                if is_same_side:
                    reject = RiskReject(
                        reason=RejectReason.DUPLICATE_POSITION,
                        details=f"Already have {signal.side.value} position on {signal.pair}",
                        suggested_action="wait",
                    )
                    self._log_decision(
                        signal, None, reject, mark_price, funding_rate,
                        account_value, available_balance, open_positions, cb_status
                    )
                    return reject

        # 4. Mode-specific processing
        if self.risk_mode == RiskMode.MANAGED:
            return self._evaluate_managed(
                signal, account_value, available_balance, open_positions,
                asset_meta, candles, funding_rate, mark_price, cb_status,
            )
        else:
            return self._evaluate_manual(
                signal, account_value, available_balance, open_positions,
                asset_meta, funding_rate, mark_price, cb_status,
            )

    def _evaluate_manual(
        self,
        signal: TradingSignal,
        account_value: Decimal,
        available_balance: Decimal,
        open_positions: list[Position],
        asset_meta: dict,
        funding_rate: Decimal,
        mark_price: Decimal,
        cb_status: CircuitBreakerStatus,
    ) -> TradeOrder | RiskReject:
        """MANUAL mode: validate signal parameters against risk limits."""
        entry_price = signal.entry_price or mark_price
        max_leverage_coin = asset_meta.get("maxLeverage", 50)
        only_isolated = asset_meta.get("onlyIsolated", False)

        # Check leverage against profile
        if signal.leverage > self.profile.max_leverage:
            reject = RiskReject(
                reason=RejectReason.LEVERAGE_EXCEEDED,
                details=f"Leverage {signal.leverage} > profile max {self.profile.max_leverage}",
                suggested_action="reduce_risk",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return reject

        # Check leverage against coin max
        if signal.leverage > max_leverage_coin:
            reject = RiskReject(
                reason=RejectReason.LEVERAGE_EXCEEDED,
                details=f"Leverage {signal.leverage} > coin max {max_leverage_coin}",
                suggested_action="reduce_risk",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return reject

        # Check max positions
        if len(open_positions) >= self.profile.max_open_positions:
            reject = RiskReject(
                reason=RejectReason.MAX_POSITIONS_REACHED,
                details=f"Already have {len(open_positions)} positions (max {self.profile.max_open_positions})",
                suggested_action="close_positions",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return reject

        # Estimate liquidation price for new position
        estimated_liq = self.calculator.estimate_liquidation_price(
            entry_price, signal.leverage, signal.side.value
        )

        # Calculate risk from signal's SL
        if signal.stop_loss:
            stop_distance = abs(entry_price - signal.stop_loss)
            risk_amount = signal.size * stop_distance

            # Validate stop vs estimated liquidation
            if not self.calculator.validate_stop_vs_liquidation(
                signal.stop_loss, estimated_liq, entry_price, signal.side.value
            ):
                reject = RiskReject(
                    reason=RejectReason.LIQUIDATION_TOO_CLOSE,
                    details=f"Stop-loss {signal.stop_loss} beyond estimated liquidation {estimated_liq:.2f}",
                    suggested_action="reduce_risk",
                )
                self._log_decision(
                    signal, None, reject, mark_price, funding_rate,
                    account_value, available_balance, open_positions, cb_status
                )
                return reject
        else:
            # No SL = max risk (full position value)
            risk_amount = signal.size * entry_price

        risk_pct = risk_amount / account_value if account_value > 0 else Decimal("1")

        # Check cumulative risk
        cum_risk = self.calculator.calculate_cumulative_risk(
            open_positions, risk_amount, signal.pair, account_value
        )
        if not cum_risk.within_limit:
            reject = RiskReject(
                reason=RejectReason.RISK_BUDGET_EXCEEDED,
                details=f"Cumulative risk {cum_risk.risk_pct:.1%} > max {self.profile.max_cumulative_risk:.1%}",
                suggested_action="reduce_risk",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return reject

        # Check margin
        margin_required = (signal.size * entry_price) / Decimal(str(signal.leverage))
        if margin_required > available_balance:
            reject = RiskReject(
                reason=RejectReason.INSUFFICIENT_MARGIN,
                details=f"Required ${margin_required:.2f} > available ${available_balance:.2f}",
                suggested_action="reduce_risk",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return reject

        # Check min order
        notional = signal.size * entry_price
        if notional < self.hl_config.min_order_value:
            reject = RiskReject(
                reason=RejectReason.POSITION_TOO_SMALL,
                details=f"Order value ${notional:.2f} < min ${self.hl_config.min_order_value}",
                suggested_action="wait",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return reject

        # Funding cost check
        if signal.stop_loss:
            funding = self.calculator.estimate_funding_cost(
                signal.size, entry_price, signal.side.value, funding_rate, risk_amount
            )
            if funding.funding_eats_risk_pct > self.profile.max_funding_risk_pct:
                reject = RiskReject(
                    reason=RejectReason.HIGH_FUNDING_COST,
                    details=f"Funding cost {funding.funding_eats_risk_pct:.0%} of risk > {self.profile.max_funding_risk_pct:.0%}",
                    suggested_action="wait",
                )
                self._log_decision(
                    signal, None, reject, mark_price, funding_rate,
                    account_value, available_balance, open_positions, cb_status
                )
                return reject

        # All checks passed
        asset_id = self.calculator.get_asset_id_from_meta(asset_meta)
        margin_mode = "isolated" if only_isolated else "cross"

        order = TradeOrder(
            coin=signal.pair,
            asset_id=asset_id,
            side=signal.side.value,
            size=signal.size,
            entry_price=entry_price,
            leverage=signal.leverage,
            margin_mode=margin_mode,
            stop_loss=signal.stop_loss or Decimal("0"),
            risk_amount=risk_amount,
            risk_pct=risk_pct,
            cumulative_risk_after=cum_risk.risk_pct,
            estimated_liquidation=estimated_liq,
            estimated_commission=notional * self.hl_config.taker_fee * 2,
            estimated_funding_24h=Decimal("0"),
            margin_required=margin_required,
            risk_mode=RiskMode.MANUAL,
            size_source="signal",
            sl_source="signal" if signal.stop_loss else "none",
            calculation_details={
                "mode": "manual",
                "cb_status": cb_status.level,
                "only_isolated": only_isolated,
            },
        )

        self._log_decision(
            signal, order, None, mark_price, funding_rate,
            account_value, available_balance, open_positions, cb_status
        )

        return order

    def _evaluate_managed(
        self,
        signal: TradingSignal,
        account_value: Decimal,
        available_balance: Decimal,
        open_positions: list[Position],
        asset_meta: dict,
        candles: list[dict],
        funding_rate: Decimal,
        mark_price: Decimal,
        cb_status: CircuitBreakerStatus,
    ) -> TradeOrder | RiskReject:
        """MANAGED mode: calculate optimal position from risk budget."""
        entry_price = signal.entry_price or mark_price
        sz_decimals = asset_meta.get("szDecimals", 0)
        max_leverage_coin = asset_meta.get("maxLeverage", 50)
        only_isolated = asset_meta.get("onlyIsolated", False)

        # Check max positions
        if len(open_positions) >= self.profile.max_open_positions:
            reject = RiskReject(
                reason=RejectReason.MAX_POSITIONS_REACHED,
                details=f"Already have {len(open_positions)} positions",
                suggested_action="close_positions",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return reject

        # Calculate ATR
        atr: Decimal | None = None
        try:
            atr = self.calculator.calculate_atr(candles)
        except ValueError as e:
            reject = RiskReject(
                reason=RejectReason.ATR_UNAVAILABLE,
                details=str(e),
                suggested_action="wait",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return reject

        if atr <= 0:
            reject = RiskReject(
                reason=RejectReason.ATR_UNAVAILABLE,
                details="ATR is zero (no volatility)",
                suggested_action="wait",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return reject

        # Calculate stop-loss
        stop_result = self.calculator.calculate_stop_loss(
            entry_price, signal.side.value, atr, signal.horizon
        )

        # Select leverage
        leverage_result = self.calculator.select_leverage(
            stop_result.distance_pct, max_leverage_coin
        )

        # Estimate liquidation and validate stop
        estimated_liq = self.calculator.estimate_liquidation_price(
            entry_price, leverage_result.leverage, signal.side.value
        )

        if not self.calculator.validate_stop_vs_liquidation(
            stop_result.price, estimated_liq, entry_price, signal.side.value
        ):
            # Reduce leverage to make stop valid
            leverage_result = self.calculator.select_leverage_for_stop(
                stop_result.price, entry_price, signal.side.value, max_leverage_coin
            )
            estimated_liq = self.calculator.estimate_liquidation_price(
                entry_price, leverage_result.leverage, signal.side.value
            )

        # Check cumulative risk budget BEFORE sizing
        cum_risk_preview = self.calculator.calculate_cumulative_risk(
            open_positions, Decimal("0"), signal.pair, account_value
        )
        max_new_risk = cum_risk_preview.available_budget

        if max_new_risk <= 0:
            reject = RiskReject(
                reason=RejectReason.RISK_BUDGET_EXCEEDED,
                details="No risk budget available",
                suggested_action="close_positions",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status, atr
            )
            return reject

        # Calculate position size with budget constraint
        size_result = self.calculator.calculate_position_size(
            account_value=account_value,
            available_balance=available_balance,
            entry_price=entry_price,
            stop_price=stop_result.price,
            leverage=leverage_result.leverage,
            sz_decimals=sz_decimals,
            confidence=signal.confidence,
            risk_multiplier=cb_status.risk_multiplier,
            max_risk_amount=max_new_risk,
        )

        if isinstance(size_result, RiskReject):
            self._log_decision(
                signal, None, size_result, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status, atr
            )
            return size_result

        # Final cumulative risk check with actual size
        cum_risk = self.calculator.calculate_cumulative_risk(
            open_positions, size_result.risk_amount, signal.pair, account_value
        )
        if not cum_risk.within_limit:
            reject = RiskReject(
                reason=RejectReason.RISK_BUDGET_EXCEEDED,
                details=f"Cumulative risk {cum_risk.risk_pct:.1%} > max",
                suggested_action="reduce_risk",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status, atr
            )
            return reject

        # Funding cost check
        funding = self.calculator.estimate_funding_cost(
            size_result.size, entry_price, signal.side.value,
            funding_rate, size_result.risk_amount
        )
        if funding.funding_eats_risk_pct > self.profile.max_funding_risk_pct:
            reject = RiskReject(
                reason=RejectReason.HIGH_FUNDING_COST,
                details=f"Funding {funding.funding_eats_risk_pct:.0%} > {self.profile.max_funding_risk_pct:.0%}",
                suggested_action="wait",
            )
            self._log_decision(
                signal, None, reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status, atr
            )
            return reject

        asset_id = self.calculator.get_asset_id_from_meta(asset_meta)
        margin_mode = "isolated" if only_isolated else "cross"

        order = TradeOrder(
            coin=signal.pair,
            asset_id=asset_id,
            side=signal.side.value,
            size=size_result.size,
            entry_price=entry_price,
            leverage=leverage_result.leverage,
            margin_mode=margin_mode,
            stop_loss=stop_result.price,
            risk_amount=size_result.risk_amount,
            risk_pct=size_result.risk_pct,
            cumulative_risk_after=cum_risk.risk_pct,
            estimated_liquidation=estimated_liq,
            estimated_commission=size_result.commission_estimate,
            estimated_funding_24h=funding.projected_24h,
            margin_required=size_result.margin_required,
            risk_mode=RiskMode.MANAGED,
            size_source="calculated",
            sl_source="calculated",
            calculation_details={
                "mode": "managed",
                "atr": str(atr),
                "atr_multiplier": str(stop_result.atr_multiplier),
                "stop_distance_pct": str(stop_result.distance_pct),
                "leverage_reason": leverage_result.reason,
                "confidence": signal.confidence,
                "cb_multiplier": str(cb_status.risk_multiplier),
                "only_isolated": only_isolated,
            },
        )

        self._log_decision(
            signal, order, None, mark_price, funding_rate,
            account_value, available_balance, open_positions, cb_status, atr
        )

        return order

    def _log_decision(
        self,
        signal: TradingSignal,
        order: TradeOrder | None,
        reject: RiskReject | None,
        mark_price: Decimal,
        funding_rate: Decimal,
        account_value: Decimal,
        available_balance: Decimal,
        open_positions: list[Position],
        cb_status: CircuitBreakerStatus,
        atr_value: Decimal | None = None,
    ) -> None:
        """Create decision log for audit trail."""
        cum_risk_before = sum(p.risk_amount or Decimal("0") for p in open_positions)
        cum_risk_before_pct = (
            cum_risk_before / account_value if account_value > 0 else Decimal("0")
        )

        self._last_decision_log = RiskDecisionLog(
            timestamp=datetime.now(timezone.utc),
            risk_mode=self.risk_mode,
            signal_source=signal.source,
            coin=signal.pair,
            side=signal.side.value,
            decision="approved" if order else "rejected",
            reject_reason=reject.reason if reject else None,
            input_size=signal.size,
            input_leverage=signal.leverage,
            input_stop_loss=signal.stop_loss,
            output_size=order.size if order else None,
            output_leverage=order.leverage if order else None,
            output_stop_loss=order.stop_loss if order else None,
            mark_price=mark_price,
            atr_value=atr_value,
            funding_rate=funding_rate,
            risk_per_trade_pct=self.profile.risk_per_trade,
            cumulative_risk_before_pct=cum_risk_before_pct,
            cumulative_risk_after_pct=order.cumulative_risk_after if order else None,
            open_positions_count=len(open_positions),
            consecutive_losses=cb_status.consecutive_losses,
            daily_pnl_pct=cb_status.daily_loss_pct,
            account_value=account_value,
            available_balance=available_balance,
            estimated_liquidation=order.estimated_liquidation if order else None,
        )

    def _get_candle_interval(self, horizon: SignalHorizon) -> str:
        """Get candle interval for horizon."""
        return ATR_SETTINGS[horizon]["interval"]

    def get_decision_log(self) -> RiskDecisionLog | None:
        """Get the last evaluation's decision log."""
        return self._last_decision_log
