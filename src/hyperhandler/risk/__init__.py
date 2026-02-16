"""Risk management module."""

from hyperhandler.risk.calculator import RiskCalculator
from hyperhandler.risk.circuit_breaker import CircuitBreaker
from hyperhandler.risk.config import (
    ATR_SETTINGS,
    CORRELATION_MAP,
    HL_MAINTENANCE_MARGIN,
    HLConfig,
    MAX_CONFIDENCE_FACTOR,
    MIN_CONFIDENCE_FACTOR,
    RISK_PROFILES,
    RiskProfile,
)

__all__ = [
    # Calculator
    "RiskCalculator",
    # Circuit Breaker
    "CircuitBreaker",
    # Config
    "ATR_SETTINGS",
    "CORRELATION_MAP",
    "HL_MAINTENANCE_MARGIN",
    "HLConfig",
    "MAX_CONFIDENCE_FACTOR",
    "MIN_CONFIDENCE_FACTOR",
    "RISK_PROFILES",
    "RiskProfile",
]
