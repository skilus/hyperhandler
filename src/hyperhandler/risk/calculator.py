"""Risk calculations: ATR, position sizing, leverage selection."""

from decimal import ROUND_DOWN, Decimal

from hyperhandler.models.order import Position
from hyperhandler.models.risk import (
    CumulativeRiskResult,
    FundingEstimate,
    LeverageResult,
    PositionSizeResult,
    RejectReason,
    RiskReject,
    StopLossResult,
)
from hyperhandler.models.signal import SignalHorizon
from hyperhandler.risk.config import (
    ATR_SETTINGS,
    CORRELATION_MAP,
    HL_MAINTENANCE_MARGIN,
    MAX_CONFIDENCE_FACTOR,
    MIN_CONFIDENCE_FACTOR,
    HLConfig,
    RiskProfile,
)


class RiskCalculator:
    """Pure calculation functions for risk management."""

    def __init__(self, profile: RiskProfile, hl_config: HLConfig):
        self.profile = profile
        self.hl_config = hl_config

    def calculate_atr(
        self,
        candles: list[dict],
        period: int = 14,
    ) -> Decimal:
        """Calculate ATR using EMA method (pure Python, no numpy).

        Args:
            candles: HL candles with {o, h, l, c} fields
            period: ATR period (default 14)

        Returns:
            ATR value as Decimal

        Raises:
            ValueError: If insufficient candles
        """
        if len(candles) < 2:
            raise ValueError("Need at least 2 candles for ATR")

        true_ranges: list[Decimal] = []
        for i in range(1, len(candles)):
            high = Decimal(str(candles[i]["h"]))
            low = Decimal(str(candles[i]["l"]))
            prev_close = Decimal(str(candles[i - 1]["c"]))

            tr = max(
                high - low,
                abs(high - prev_close),
                abs(low - prev_close),
            )
            true_ranges.append(tr)

        if len(true_ranges) < period:
            # Not enough data for full EMA, use simple average
            return sum(true_ranges) / len(true_ranges)

        # EMA calculation
        alpha = Decimal("2") / (Decimal(str(period)) + Decimal("1"))
        ema = true_ranges[0]
        for tr in true_ranges[1:]:
            ema = alpha * tr + (Decimal("1") - alpha) * ema

        return ema

    def calculate_stop_loss(
        self,
        entry_price: Decimal,
        side: str,
        atr: Decimal,
        horizon: SignalHorizon,
    ) -> StopLossResult:
        """Calculate ATR-based stop-loss price.

        Args:
            entry_price: Entry price
            side: "long" or "short"
            atr: ATR value
            horizon: Signal horizon for multiplier selection

        Returns:
            StopLossResult with price and calculation details
        """
        settings = ATR_SETTINGS[horizon]
        multiplier = settings["multiplier"]

        stop_distance = atr * multiplier
        buffer = stop_distance * self.hl_config.slippage_buffer
        stop_with_buffer = stop_distance + buffer

        if side == "long":
            stop_price = entry_price - stop_with_buffer
        else:
            stop_price = entry_price + stop_with_buffer

        return StopLossResult(
            price=stop_price,
            distance=stop_with_buffer,
            distance_pct=stop_with_buffer / entry_price,
            atr_value=atr,
            atr_multiplier=multiplier,
        )

    def estimate_liquidation_price(
        self,
        entry_price: Decimal,
        leverage: int,
        side: str,
    ) -> Decimal:
        """Estimate liquidation price for a NEW position.

        Simplified formula (HL cross margin):
        liq_distance ≈ 1/leverage - maintenance_margin

        Args:
            entry_price: Expected entry price
            leverage: Selected leverage
            side: "long" or "short"

        Returns:
            Estimated liquidation price
        """
        liq_distance_pct = (Decimal("1") / Decimal(str(leverage))) - HL_MAINTENANCE_MARGIN
        liq_distance_pct = max(Decimal("0.01"), liq_distance_pct)  # Floor at 1%

        if side == "long":
            return entry_price * (Decimal("1") - liq_distance_pct)
        else:
            return entry_price * (Decimal("1") + liq_distance_pct)

    def validate_stop_vs_liquidation(
        self,
        stop_price: Decimal,
        liquidation_price: Decimal,
        entry_price: Decimal,
        side: str,
    ) -> bool:
        """Validate stop-loss is closer to entry than liquidation.

        Args:
            stop_price: Stop-loss price
            liquidation_price: Liquidation price (actual or estimated)
            entry_price: Entry price
            side: "long" or "short"

        Returns:
            True if valid, False if stop is beyond liquidation
        """
        if side == "long":
            # For long: stop must be ABOVE liquidation
            if stop_price <= liquidation_price:
                return False
        else:
            # For short: stop must be BELOW liquidation
            if stop_price >= liquidation_price:
                return False

        # Check safety buffer (stop should be at least 2% away from liq)
        liq_buffer = abs(stop_price - liquidation_price) / entry_price
        return liq_buffer >= self.hl_config.liq_safety_buffer

    def select_leverage(
        self,
        stop_distance_pct: Decimal,
        max_leverage_coin: int,
    ) -> LeverageResult:
        """Select leverage so liquidation is beyond stop-loss.

        Args:
            stop_distance_pct: Stop distance as % of entry
            max_leverage_coin: HL max leverage for this coin

        Returns:
            LeverageResult with selected leverage and reasoning
        """
        safety_factor = Decimal("1.5")

        if stop_distance_pct <= 0:
            max_safe = self.profile.max_leverage
        else:
            max_safe = int(Decimal("1") / (stop_distance_pct * safety_factor))
            max_safe = max(1, max_safe)

        leverage = min(
            max_safe,
            max_leverage_coin,
            self.profile.max_leverage,
        )

        reason = []
        if leverage == max_safe:
            reason.append("safe_for_stop")
        if leverage == max_leverage_coin:
            reason.append("coin_max")
        if leverage == self.profile.max_leverage:
            reason.append("config_max")

        return LeverageResult(
            leverage=leverage,
            max_safe=max_safe,
            max_coin=max_leverage_coin,
            max_config=self.profile.max_leverage,
            reason="+".join(reason) if reason else "default",
        )

    def select_leverage_for_stop(
        self,
        stop_price: Decimal,
        entry_price: Decimal,
        side: str,
        max_leverage_coin: int,
    ) -> LeverageResult:
        """Select leverage that makes the given stop valid.

        Used when ATR-based stop requires lower leverage.

        Args:
            stop_price: Stop-loss price
            entry_price: Entry price
            side: "long" or "short"
            max_leverage_coin: HL max leverage for this coin

        Returns:
            LeverageResult with adjusted leverage
        """
        stop_distance_pct = abs(entry_price - stop_price) / entry_price

        # Liquidation must be further than stop + safety buffer
        required_liq_distance = stop_distance_pct + self.hl_config.liq_safety_buffer

        # liq_distance ≈ 1/leverage - maintenance
        # leverage ≈ 1 / (liq_distance + maintenance)
        max_safe = int(Decimal("1") / (required_liq_distance + HL_MAINTENANCE_MARGIN))
        max_safe = max(1, max_safe)

        leverage = min(max_safe, max_leverage_coin, self.profile.max_leverage)

        return LeverageResult(
            leverage=leverage,
            max_safe=max_safe,
            max_coin=max_leverage_coin,
            max_config=self.profile.max_leverage,
            reason="adjusted_for_stop",
        )

    def calculate_position_size(
        self,
        account_value: Decimal,
        available_balance: Decimal,
        entry_price: Decimal,
        stop_price: Decimal,
        leverage: int,
        sz_decimals: int,
        confidence: float | None = None,
        risk_multiplier: Decimal = Decimal("1.0"),
        max_risk_amount: Decimal | None = None,
    ) -> PositionSizeResult | RiskReject:
        """Calculate position size based on risk budget.

        Args:
            account_value: Total account equity
            available_balance: Free margin
            entry_price: Entry price
            stop_price: Stop-loss price
            leverage: Selected leverage
            sz_decimals: Asset size decimals (for rounding)
            confidence: Signal confidence (0.0-1.0)
            risk_multiplier: Circuit breaker multiplier
            max_risk_amount: Budget constraint (max risk $)

        Returns:
            PositionSizeResult or RiskReject if position too small
        """
        # Clamp confidence factor
        if confidence is not None:
            confidence_factor = max(
                MIN_CONFIDENCE_FACTOR,
                min(MAX_CONFIDENCE_FACTOR, Decimal(str(confidence))),
            )
        else:
            confidence_factor = MAX_CONFIDENCE_FACTOR

        risk_pct = self.profile.risk_per_trade * confidence_factor * risk_multiplier
        risk_amount = account_value * risk_pct

        # Apply budget constraint
        if max_risk_amount is not None and risk_amount > max_risk_amount:
            risk_amount = max_risk_amount

        stop_distance = abs(entry_price - stop_price)
        if stop_distance == 0:
            return RiskReject(
                reason=RejectReason.ATR_UNAVAILABLE,
                details="Stop distance is zero",
                suggested_action="wait",
            )

        raw_size = risk_amount / stop_distance
        notional = raw_size * entry_price
        commission = notional * self.hl_config.taker_fee * Decimal("2")

        # Adjust for commission
        adjusted_risk = risk_amount - commission
        if adjusted_risk <= 0:
            return RiskReject(
                reason=RejectReason.POSITION_TOO_SMALL,
                details="Risk amount doesn't cover commission",
                suggested_action="wait",
            )

        adjusted_size = adjusted_risk / stop_distance

        # Check margin constraint
        margin_required = (adjusted_size * entry_price) / Decimal(str(leverage))
        if margin_required > available_balance:
            max_size = (available_balance * Decimal(str(leverage))) / entry_price
            adjusted_size = min(adjusted_size, max_size)
            margin_required = (adjusted_size * entry_price) / Decimal(str(leverage))

        # Round down to szDecimals
        adjusted_size = self._round_down(adjusted_size, sz_decimals)

        final_notional = adjusted_size * entry_price
        final_risk = adjusted_size * stop_distance
        final_commission = final_notional * self.hl_config.taker_fee * Decimal("2")

        # Check minimum order
        if final_notional < self.hl_config.min_order_value:
            return RiskReject(
                reason=RejectReason.POSITION_TOO_SMALL,
                details=f"Order ${final_notional:.2f} < min ${self.hl_config.min_order_value}",
                suggested_action="wait",
            )

        return PositionSizeResult(
            size=adjusted_size,
            notional=final_notional,
            margin_required=margin_required,
            risk_amount=final_risk,
            risk_pct=final_risk / account_value if account_value > 0 else Decimal("0"),
            commission_estimate=final_commission,
        )

    def calculate_cumulative_risk(
        self,
        open_positions: list[Position],
        new_risk_amount: Decimal,
        new_coin: str,
        account_value: Decimal,
    ) -> CumulativeRiskResult:
        """Calculate cumulative portfolio risk with correlation adjustment.

        Args:
            open_positions: List of open positions
            new_risk_amount: Risk amount of new position (0 for preview)
            new_coin: Coin of new position
            account_value: Total account equity

        Returns:
            CumulativeRiskResult with risk metrics
        """
        # Build correlation groups from positions
        groups: dict[str, list[str]] = {}
        for pos in open_positions:
            group = self._get_correlation_group(pos.coin)
            if group not in groups:
                groups[group] = []
            groups[group].append(pos.coin)

        # Add new coin if we're actually adding risk
        if new_risk_amount > 0:
            new_group = self._get_correlation_group(new_coin)
            if new_group not in groups:
                groups[new_group] = []
            if new_coin not in groups[new_group]:
                groups[new_group].append(new_coin)

        # Sum raw risk
        total_risk = sum(pos.risk_amount or Decimal("0") for pos in open_positions)

        # Calculate adjusted risk with correlation penalty
        adjusted_risk = Decimal("0")
        for group_name, coins in groups.items():
            group_positions = [p for p in open_positions if p.coin in coins]
            group_risk = sum(p.risk_amount or Decimal("0") for p in group_positions)

            n = len(coins)
            if n > 1:
                penalty = Decimal("1") + Decimal(str(n - 1)) * self.profile.correlation_factor
                group_risk = group_risk * penalty

            adjusted_risk += group_risk

        # Add cascade buffer for correlated groups
        cascade_buffer = Decimal("0")
        for group_name, coins in groups.items():
            if len(coins) > 1:
                group_positions = [p for p in open_positions if p.coin in coins]
                group_risk = sum(p.risk_amount or Decimal("0") for p in group_positions)
                cascade_buffer += group_risk * Decimal("0.1")

        total_adjusted = adjusted_risk + cascade_buffer + new_risk_amount
        max_risk = account_value * self.profile.max_cumulative_risk

        return CumulativeRiskResult(
            raw_risk=total_risk + new_risk_amount,
            adjusted_risk=total_adjusted,
            risk_pct=total_adjusted / account_value if account_value > 0 else Decimal("0"),
            available_budget=max(Decimal("0"), max_risk - adjusted_risk - cascade_buffer),
            within_limit=total_adjusted <= max_risk,
            correlation_groups=groups,
        )

    def estimate_funding_cost(
        self,
        size: Decimal,
        entry_price: Decimal,
        side: str,
        funding_rate: Decimal,
        risk_amount: Decimal,
        hold_hours: int = 24,
    ) -> FundingEstimate:
        """Estimate funding costs/income.

        Args:
            size: Position size
            entry_price: Entry price
            side: "long" or "short"
            funding_rate: Hourly funding rate
            risk_amount: Risk amount for % calculation
            hold_hours: Expected hold duration

        Returns:
            FundingEstimate with cost projections
        """
        notional = size * entry_price
        hourly_payment = notional * funding_rate

        if side == "long":
            projected_cost = hourly_payment * Decimal(str(hold_hours))
        else:
            projected_cost = -hourly_payment * Decimal(str(hold_hours))

        funding_eats_risk_pct = (
            max(Decimal("0"), projected_cost) / risk_amount
            if risk_amount > 0
            else Decimal("0")
        )

        return FundingEstimate(
            hourly_rate=funding_rate,
            hourly_cost=abs(hourly_payment) if projected_cost > 0 else Decimal("0"),
            hourly_income=abs(hourly_payment) if projected_cost < 0 else Decimal("0"),
            projected_24h=projected_cost,
            funding_eats_risk_pct=funding_eats_risk_pct,
        )

    def _round_down(self, value: Decimal, decimals: int) -> Decimal:
        """Round down to specified decimals (HL requirement)."""
        assert decimals >= 0, f"decimals must be >= 0, got {decimals}"

        if decimals == 0:
            return value.to_integral_value(rounding=ROUND_DOWN)
        quantize_str = "0." + "0" * decimals
        return value.quantize(Decimal(quantize_str), rounding=ROUND_DOWN)

    def _get_correlation_group(self, coin: str) -> str:
        """Get correlation group for a coin."""
        for group, coins in CORRELATION_MAP.items():
            if coin in coins:
                return group
        return f"independent-{coin}"

    def get_asset_id_from_meta(self, asset_meta: dict) -> int:
        """Extract asset ID from metadata."""
        return asset_meta.get("_asset_id", 0)
