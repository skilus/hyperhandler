"""Utility functions for hlhandler."""

import re


def normalize_private_key(key: str) -> str:
    """Normalize a private key to standard format.

    Args:
        key: Private key string, with or without 0x prefix.

    Returns:
        Private key with 0x prefix, lowercase.

    Raises:
        ValueError: If the key is not valid hex or wrong length.
    """
    # Remove 0x prefix if present
    clean_key = key.lower().strip()
    if clean_key.startswith("0x"):
        clean_key = clean_key[2:]

    # Validate hex format
    if not re.match(r"^[0-9a-f]{64}$", clean_key):
        raise ValueError("Private key must be 64 hex characters (32 bytes)")

    return f"0x{clean_key}"


def validate_private_key(key: str) -> bool:
    """Check if a string is a valid private key.

    Args:
        key: Private key string to validate.

    Returns:
        True if valid, False otherwise.
    """
    try:
        normalize_private_key(key)
        return True
    except ValueError:
        return False


def mask_key(key: str, visible_chars: int = 4) -> str:
    """Mask a private key for display.

    Args:
        key: Private key to mask.
        visible_chars: Number of characters to show at start and end.

    Returns:
        Masked key like "0x1234...abcd".
    """
    if len(key) <= visible_chars * 2 + 3:
        return "*" * len(key)

    return f"{key[:visible_chars + 2]}...{key[-visible_chars:]}"
