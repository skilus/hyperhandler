"""Unit tests for RiskCalculator."""

from decimal import Decimal

import pytest

from hyperhandler.models.order import Position
from hyperhandler.models.risk import RejectReason, RiskReject
from hyperhandler.models.signal import SignalHorizon
from hyperhandler.risk import HLConfig, RiskProfile, RISK_PROFILES, RiskCalculator
from hyperhandler.models.risk import RiskLevel


@pytest.fixture
def calculator() -> RiskCalculator:
    """Create calculator with MEDIUM risk profile."""
    profile = RISK_PROFILES[RiskLevel.MEDIUM]
    hl_config = HLConfig()
    return RiskCalculator(profile, hl_config)


@pytest.fixture
def sample_candles() -> list[dict]:
    """Generate sample candles for ATR testing."""
    # 20 candles with known values for predictable ATR
    base_price = Decimal("100")
    candles = []
    for i in range(20):
        high = base_price + Decimal("2") + Decimal(str(i % 3))
        low = base_price - Decimal("2") - Decimal(str(i % 2))
        close = base_price + Decimal(str(i % 2))
        candles.append({
            "o": str(base_price),
            "h": str(high),
            "l": str(low),
            "c": str(close),
        })
    return candles


class TestCalculateATR:
    """Tests for ATR calculation."""

    def test_calculate_atr_basic(self, calculator: RiskCalculator, sample_candles: list[dict]):
        """ATR should return positive value for valid candles."""
        atr = calculator.calculate_atr(sample_candles)
        assert atr > 0
        assert isinstance(atr, Decimal)

    def test_calculate_atr_insufficient_candles(self, calculator: RiskCalculator):
        """ATR should raise error with less than 2 candles."""
        with pytest.raises(ValueError, match="Need at least 2 candles"):
            calculator.calculate_atr([{"o": "100", "h": "101", "l": "99", "c": "100"}])

    def test_calculate_atr_two_candles(self, calculator: RiskCalculator):
        """ATR should work with exactly 2 candles."""
        candles = [
            {"o": "100", "h": "102", "l": "98", "c": "101"},
            {"o": "101", "h": "104", "l": "99", "c": "103"},
        ]
        atr = calculator.calculate_atr(candles)
        # TR = max(104-99, |104-101|, |99-101|) = max(5, 3, 2) = 5
        assert atr == Decimal("5")

    def test_calculate_atr_uses_ema_for_long_series(self, calculator: RiskCalculator):
        """ATR should use EMA when enough data points."""
        # Create 20 candles with consistent TR of 4
        candles = []
        for i in range(20):
            candles.append({
                "o": "100",
                "h": "102",
                "l": "98",
                "c": "100",
            })
        atr = calculator.calculate_atr(candles, period=14)
        # With consistent TR=4, EMA should converge to ~4
        assert Decimal("3.5") < atr < Decimal("4.5")

    def test_calculate_atr_short_series_uses_sma(self, calculator: RiskCalculator):
        """ATR should use simple average when series shorter than period."""
        candles = [
            {"o": "100", "h": "102", "l": "98", "c": "100"},  # TR will be calculated from next
            {"o": "100", "h": "103", "l": "97", "c": "101"},  # TR = 6
            {"o": "101", "h": "105", "l": "99", "c": "102"},  # TR = 6
        ]
        atr = calculator.calculate_atr(candles, period=14)
        # Only 2 TR values, should be average
        assert atr == Decimal("6")


class TestCalculateStopLoss:
    """Tests for stop-loss calculation."""

    def test_calculate_stop_loss_long(self, calculator: RiskCalculator):
        """Stop-loss for long should be below entry."""
        entry = Decimal("100")
        atr = Decimal("5")
        result = calculator.calculate_stop_loss(entry, "long", atr, SignalHorizon.INTRADAY)

        assert result.price < entry
        assert result.distance > 0
        assert result.atr_value == atr
        assert result.atr_multiplier == Decimal("1.5")  # INTRADAY multiplier

    def test_calculate_stop_loss_short(self, calculator: RiskCalculator):
        """Stop-loss for short should be above entry."""
        entry = Decimal("100")
        atr = Decimal("5")
        result = calculator.calculate_stop_loss(entry, "short", atr, SignalHorizon.INTRADAY)

        assert result.price > entry
        assert result.distance > 0

    def test_calculate_stop_loss_horizon_multipliers(self, calculator: RiskCalculator):
        """Different horizons should use different multipliers."""
        entry = Decimal("100")
        atr = Decimal("5")

        scalp = calculator.calculate_stop_loss(entry, "long", atr, SignalHorizon.SCALP)
        intraday = calculator.calculate_stop_loss(entry, "long", atr, SignalHorizon.INTRADAY)
        swing = calculator.calculate_stop_loss(entry, "long", atr, SignalHorizon.SWING)
        position = calculator.calculate_stop_loss(entry, "long", atr, SignalHorizon.POSITION)

        # Scalp has tightest stop, position has widest
        assert scalp.distance < intraday.distance < swing.distance < position.distance
        assert scalp.atr_multiplier == Decimal("1.2")
        assert intraday.atr_multiplier == Decimal("1.5")
        assert swing.atr_multiplier == Decimal("2.0")
        assert position.atr_multiplier == Decimal("2.5")

    def test_calculate_stop_loss_includes_slippage_buffer(self, calculator: RiskCalculator):
        """Stop distance should include slippage buffer."""
        entry = Decimal("100")
        atr = Decimal("10")
        result = calculator.calculate_stop_loss(entry, "long", atr, SignalHorizon.INTRADAY)

        # Distance should be ATR * multiplier * (1 + slippage_buffer)
        base_distance = atr * Decimal("1.5")
        expected_distance = base_distance + base_distance * calculator.hl_config.slippage_buffer
        assert result.distance == expected_distance


class TestEstimateLiquidationPrice:
    """Tests for liquidation price estimation."""

    def test_estimate_liquidation_price_long(self, calculator: RiskCalculator):
        """Liquidation for long should be below entry."""
        entry = Decimal("100")
        liq = calculator.estimate_liquidation_price(entry, leverage=10, side="long")

        assert liq < entry
        # At 10x, liq distance ~= 1/10 - 0.5% = 9.5%
        expected_distance = (Decimal("1") / Decimal("10")) - Decimal("0.005")
        expected_liq = entry * (Decimal("1") - expected_distance)
        assert liq == expected_liq

    def test_estimate_liquidation_price_short(self, calculator: RiskCalculator):
        """Liquidation for short should be above entry."""
        entry = Decimal("100")
        liq = calculator.estimate_liquidation_price(entry, leverage=10, side="short")

        assert liq > entry

    def test_estimate_liquidation_price_high_leverage(self, calculator: RiskCalculator):
        """Higher leverage = closer liquidation."""
        entry = Decimal("100")
        liq_5x = calculator.estimate_liquidation_price(entry, leverage=5, side="long")
        liq_20x = calculator.estimate_liquidation_price(entry, leverage=20, side="long")

        assert liq_20x > liq_5x  # 20x liq is closer to entry

    def test_estimate_liquidation_price_floor(self, calculator: RiskCalculator):
        """Liquidation distance should have minimum floor."""
        entry = Decimal("100")
        # Very high leverage would give tiny distance, but should be floored
        liq = calculator.estimate_liquidation_price(entry, leverage=100, side="long")

        # Distance should be at least 1%
        distance_pct = (entry - liq) / entry
        assert distance_pct >= Decimal("0.01")


class TestValidateStopVsLiquidation:
    """Tests for stop vs liquidation validation."""

    def test_validate_stop_vs_liquidation_valid_long(self, calculator: RiskCalculator):
        """Valid: stop above liquidation for long."""
        entry = Decimal("100")
        stop = Decimal("95")
        liq = Decimal("90")

        assert calculator.validate_stop_vs_liquidation(stop, liq, entry, "long") is True

    def test_validate_stop_vs_liquidation_invalid_long(self, calculator: RiskCalculator):
        """Invalid: stop at or below liquidation for long."""
        entry = Decimal("100")
        stop = Decimal("89")
        liq = Decimal("90")

        assert calculator.validate_stop_vs_liquidation(stop, liq, entry, "long") is False

    def test_validate_stop_vs_liquidation_valid_short(self, calculator: RiskCalculator):
        """Valid: stop below liquidation for short."""
        entry = Decimal("100")
        stop = Decimal("105")
        liq = Decimal("110")

        assert calculator.validate_stop_vs_liquidation(stop, liq, entry, "short") is True

    def test_validate_stop_vs_liquidation_invalid_short(self, calculator: RiskCalculator):
        """Invalid: stop at or above liquidation for short."""
        entry = Decimal("100")
        stop = Decimal("111")
        liq = Decimal("110")

        assert calculator.validate_stop_vs_liquidation(stop, liq, entry, "short") is False

    def test_validate_stop_vs_liquidation_requires_buffer(self, calculator: RiskCalculator):
        """Stop should be at least 2% away from liquidation."""
        entry = Decimal("100")
        liq = Decimal("90")
        # Stop just 1% away from liq - should fail buffer check
        stop = Decimal("90.5")

        assert calculator.validate_stop_vs_liquidation(stop, liq, entry, "long") is False

        # Stop 3% away from liq - should pass
        stop = Decimal("93")
        assert calculator.validate_stop_vs_liquidation(stop, liq, entry, "long") is True


class TestSelectLeverage:
    """Tests for leverage selection."""

    def test_select_leverage_safe_for_stop(self, calculator: RiskCalculator):
        """Leverage should be safe for stop distance."""
        stop_distance_pct = Decimal("0.15")  # 15% stop -> max safe = 4
        result = calculator.select_leverage(stop_distance_pct, max_leverage_coin=50)

        # With 15% stop and 1.5x safety, max safe = 1/(0.15*1.5) = 4
        assert result.leverage <= 4
        assert "safe_for_stop" in result.reason

    def test_select_leverage_capped_by_coin(self, calculator: RiskCalculator):
        """Leverage should respect coin max."""
        stop_distance_pct = Decimal("0.01")  # 1% stop -> very high safe leverage
        result = calculator.select_leverage(stop_distance_pct, max_leverage_coin=5)

        assert result.leverage == 5
        assert "coin_max" in result.reason

    def test_select_leverage_capped_by_config(self, calculator: RiskCalculator):
        """Leverage should respect profile max."""
        stop_distance_pct = Decimal("0.01")  # 1% stop
        result = calculator.select_leverage(stop_distance_pct, max_leverage_coin=50)

        # MEDIUM profile has max_leverage=10
        assert result.leverage == 10
        assert "config_max" in result.reason

    def test_select_leverage_minimum_one(self, calculator: RiskCalculator):
        """Leverage should be at least 1."""
        stop_distance_pct = Decimal("0.5")  # 50% stop -> would give <1 leverage
        result = calculator.select_leverage(stop_distance_pct, max_leverage_coin=50)

        assert result.leverage >= 1


class TestSelectLeverageForStop:
    """Tests for leverage selection based on specific stop price."""

    def test_select_leverage_for_stop_basic(self, calculator: RiskCalculator):
        """Should select leverage that keeps liq beyond stop."""
        entry = Decimal("100")
        stop = Decimal("95")  # 5% stop
        result = calculator.select_leverage_for_stop(stop, entry, "long", max_leverage_coin=50)

        # Verify the selected leverage keeps liq beyond stop
        liq = calculator.estimate_liquidation_price(entry, result.leverage, "long")
        assert liq < stop  # Liq should be further from entry than stop

    def test_select_leverage_for_stop_reason(self, calculator: RiskCalculator):
        """Reason should indicate adjustment."""
        entry = Decimal("100")
        stop = Decimal("95")
        result = calculator.select_leverage_for_stop(stop, entry, "long", max_leverage_coin=50)

        assert result.reason == "adjusted_for_stop"


class TestCalculatePositionSize:
    """Tests for position size calculation."""

    def test_calculate_position_size_normal(self, calculator: RiskCalculator):
        """Normal position sizing based on risk."""
        result = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=2,
        )

        assert isinstance(result, PositionSizeResult)
        assert result.size > 0
        assert result.notional == result.size * Decimal("100")
        # Risk should be ~2% of account (MEDIUM profile)
        assert Decimal("0.015") < result.risk_pct < Decimal("0.025")

    def test_calculate_position_size_margin_constrained(self, calculator: RiskCalculator):
        """Position should be limited by available margin."""
        result = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("100"),  # Very limited margin
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=2,
        )

        assert isinstance(result, PositionSizeResult)
        # Margin required should not exceed available
        assert result.margin_required <= Decimal("100")

    def test_calculate_position_size_too_small(self, calculator: RiskCalculator):
        """Should reject if position too small."""
        result = calculator.calculate_position_size(
            account_value=Decimal("50"),  # Very small account
            available_balance=Decimal("5"),  # Very limited margin
            entry_price=Decimal("50000"),  # BTC-like price
            stop_price=Decimal("45000"),  # 10% stop
            leverage=2,  # Low leverage
            sz_decimals=5,
        )

        # With $50 account, 2% risk = $1, 10% stop = size of 0.0002 BTC
        # Notional = 0.0002 * 50000 = $10, but after rounding might be < $10
        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.POSITION_TOO_SMALL

    def test_calculate_position_size_zero_stop_distance(self, calculator: RiskCalculator):
        """Should reject if stop distance is zero."""
        result = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("100"),  # Same as entry!
            leverage=10,
            sz_decimals=2,
        )

        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.ATR_UNAVAILABLE

    def test_calculate_position_size_confidence_scaling(self, calculator: RiskCalculator):
        """Lower confidence should reduce position size."""
        full_confidence = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=2,
            confidence=1.0,
        )

        half_confidence = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=2,
            confidence=0.5,
        )

        assert isinstance(full_confidence, PositionSizeResult)
        assert isinstance(half_confidence, PositionSizeResult)
        assert half_confidence.size < full_confidence.size

    def test_calculate_position_size_confidence_clamp(self, calculator: RiskCalculator):
        """Confidence below 0.3 should be clamped to 0.3."""
        low_confidence = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=2,
            confidence=0.1,  # Below 0.3 minimum
        )

        clamped_confidence = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=2,
            confidence=0.3,  # At minimum
        )

        assert isinstance(low_confidence, PositionSizeResult)
        assert isinstance(clamped_confidence, PositionSizeResult)
        # Both should produce same size due to clamping
        assert low_confidence.size == clamped_confidence.size

    def test_calculate_position_size_risk_multiplier(self, calculator: RiskCalculator):
        """Risk multiplier from circuit breaker should reduce size."""
        normal = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=2,
            risk_multiplier=Decimal("1.0"),
        )

        reduced = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=2,
            risk_multiplier=Decimal("0.5"),
        )

        assert isinstance(normal, PositionSizeResult)
        assert isinstance(reduced, PositionSizeResult)
        assert reduced.size < normal.size

    def test_calculate_position_size_max_risk_budget(self, calculator: RiskCalculator):
        """Position should respect max risk budget constraint."""
        unconstrained = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=2,
        )

        constrained = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=2,
            max_risk_amount=Decimal("50"),  # Very small budget
        )

        assert isinstance(unconstrained, PositionSizeResult)
        assert isinstance(constrained, PositionSizeResult)
        assert constrained.risk_amount <= Decimal("50")
        assert constrained.size < unconstrained.size

    def test_calculate_position_size_rounding(self, calculator: RiskCalculator):
        """Size should be rounded down to szDecimals."""
        result = calculator.calculate_position_size(
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            entry_price=Decimal("100"),
            stop_price=Decimal("95"),
            leverage=10,
            sz_decimals=3,
        )

        assert isinstance(result, PositionSizeResult)
        # Check size has at most 3 decimal places
        size_str = str(result.size)
        if "." in size_str:
            decimals = len(size_str.split(".")[1])
            assert decimals <= 3


class TestCalculateCumulativeRisk:
    """Tests for cumulative portfolio risk calculation."""

    def test_cumulative_risk_single_position(self, calculator: RiskCalculator):
        """Single position should have no correlation penalty."""
        positions = [
            Position(
                coin="BTC",
                size=Decimal("1"),
                entry_price=Decimal("50000"),
                position_value=Decimal("50000"),
                unrealized_pnl=Decimal("0"),
                leverage=10,
                leverage_type="cross",
                risk_amount=Decimal("500"),
            )
        ]

        result = calculator.calculate_cumulative_risk(
            positions,
            new_risk_amount=Decimal("0"),
            new_coin="BTC",
            account_value=Decimal("10000"),
        )

        assert result.raw_risk == Decimal("500")
        assert result.within_limit is True

    def test_cumulative_risk_correlation_penalty(self, calculator: RiskCalculator):
        """Correlated positions should have penalty applied."""
        positions = [
            Position(
                coin="BTC",
                size=Decimal("1"),
                entry_price=Decimal("50000"),
                position_value=Decimal("50000"),
                unrealized_pnl=Decimal("0"),
                leverage=10,
                leverage_type="cross",
                risk_amount=Decimal("100"),
            ),
            Position(
                coin="ETH",
                size=Decimal("10"),
                entry_price=Decimal("3000"),
                position_value=Decimal("30000"),
                unrealized_pnl=Decimal("0"),
                leverage=10,
                leverage_type="cross",
                risk_amount=Decimal("100"),
            ),
        ]

        result = calculator.calculate_cumulative_risk(
            positions,
            new_risk_amount=Decimal("0"),
            new_coin="SOL",
            account_value=Decimal("10000"),
        )

        # BTC and ETH are in same correlation group
        # Adjusted risk should be higher than raw risk
        assert result.adjusted_risk > result.raw_risk

    def test_cumulative_risk_independent_coins(self, calculator: RiskCalculator):
        """Independent coins should have minimal correlation adjustment."""
        positions = [
            Position(
                coin="DOGE",  # meme group
                size=Decimal("1000"),
                entry_price=Decimal("0.1"),
                position_value=Decimal("100"),
                unrealized_pnl=Decimal("0"),
                leverage=5,
                leverage_type="cross",
                risk_amount=Decimal("50"),
            ),
        ]

        result = calculator.calculate_cumulative_risk(
            positions,
            new_risk_amount=Decimal("50"),
            new_coin="AAVE",  # defi group - different from DOGE
            account_value=Decimal("10000"),
        )

        # Different groups, minimal penalty
        assert result.correlation_groups["meme"] == ["DOGE"]
        assert "AAVE" in result.correlation_groups.get("defi", [])

    def test_cumulative_risk_exceeds_limit(self, calculator: RiskCalculator):
        """Should detect when cumulative risk exceeds limit."""
        positions = [
            Position(
                coin="BTC",
                size=Decimal("1"),
                entry_price=Decimal("50000"),
                position_value=Decimal("50000"),
                unrealized_pnl=Decimal("0"),
                leverage=10,
                leverage_type="cross",
                risk_amount=Decimal("500"),  # 5% of 10000
            ),
        ]

        result = calculator.calculate_cumulative_risk(
            positions,
            new_risk_amount=Decimal("500"),  # Another 5%
            new_coin="ETH",
            account_value=Decimal("10000"),
        )

        # MEDIUM profile max is 6%, we're at 10%+ with correlation
        assert result.within_limit is False

    def test_cumulative_risk_available_budget(self, calculator: RiskCalculator):
        """Should calculate available budget correctly."""
        positions = []  # No existing positions

        result = calculator.calculate_cumulative_risk(
            positions,
            new_risk_amount=Decimal("0"),
            new_coin="BTC",
            account_value=Decimal("10000"),
        )

        # MEDIUM profile: 6% max = $600
        assert result.available_budget == Decimal("600")


class TestEstimateFundingCost:
    """Tests for funding cost estimation."""

    def test_funding_cost_long_positive_rate(self, calculator: RiskCalculator):
        """Long pays funding when rate is positive."""
        result = calculator.estimate_funding_cost(
            size=Decimal("1"),
            entry_price=Decimal("50000"),
            side="long",
            funding_rate=Decimal("0.0001"),  # 0.01% per hour
            risk_amount=Decimal("500"),
        )

        assert result.hourly_cost > 0
        assert result.hourly_income == 0
        assert result.projected_24h > 0

    def test_funding_cost_short_positive_rate(self, calculator: RiskCalculator):
        """Short receives funding when rate is positive."""
        result = calculator.estimate_funding_cost(
            size=Decimal("1"),
            entry_price=Decimal("50000"),
            side="short",
            funding_rate=Decimal("0.0001"),
            risk_amount=Decimal("500"),
        )

        assert result.hourly_income > 0
        assert result.hourly_cost == 0
        assert result.projected_24h < 0  # Negative = income

    def test_funding_eats_risk_percentage(self, calculator: RiskCalculator):
        """Should calculate what % of risk funding eats."""
        result = calculator.estimate_funding_cost(
            size=Decimal("1"),
            entry_price=Decimal("50000"),
            side="long",
            funding_rate=Decimal("0.001"),  # High 0.1% per hour
            risk_amount=Decimal("500"),
            hold_hours=24,
        )

        # 50000 * 0.001 * 24 = 1200 cost vs 500 risk
        assert result.funding_eats_risk_pct > Decimal("1.0")  # > 100%


class TestRoundDown:
    """Tests for _round_down helper."""

    def test_round_down_basic(self, calculator: RiskCalculator):
        """Should round down to specified decimals."""
        assert calculator._round_down(Decimal("1.999"), 2) == Decimal("1.99")
        assert calculator._round_down(Decimal("1.991"), 2) == Decimal("1.99")

    def test_round_down_zero_decimals(self, calculator: RiskCalculator):
        """Should handle zero decimals."""
        assert calculator._round_down(Decimal("9.99"), 0) == Decimal("9")

    def test_round_down_negative_decimals_raises(self, calculator: RiskCalculator):
        """Should raise on negative decimals."""
        with pytest.raises(AssertionError):
            calculator._round_down(Decimal("1.23"), -1)


# Import for type hints in tests
from hyperhandler.models.risk import PositionSizeResult
