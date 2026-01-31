"""Base HTTP client for Hyperliquid API."""

import asyncio
from decimal import Decimal
from typing import Any

import httpx

from hlhandler.config import NetworkConfig


class APIError(Exception):
    """Base exception for API errors."""

    def __init__(self, message: str, status_code: int | None = None, response: Any = None):
        super().__init__(message)
        self.status_code = status_code
        self.response = response


class RateLimitError(APIError):
    """Rate limit exceeded."""

    pass


class SignatureError(APIError):
    """Invalid signature error."""

    pass


class InsufficientMarginError(APIError):
    """Insufficient margin for order."""

    pass


class AssetNotFoundError(APIError):
    """Asset/pair not found."""

    pass


class BaseClient:
    """Base HTTP client with retry logic."""

    def __init__(
        self,
        network: NetworkConfig,
        timeout: float = 30.0,
        max_retries: int = 3,
        retry_delay: float = 1.0,
    ):
        """Initialize the client.

        Args:
            network: Network configuration.
            timeout: Request timeout in seconds.
            max_retries: Maximum retry attempts.
            retry_delay: Initial delay between retries.
        """
        self.network = network
        self.timeout = timeout
        self.max_retries = max_retries
        self.retry_delay = retry_delay
        self._client: httpx.AsyncClient | None = None

    async def __aenter__(self) -> "BaseClient":
        """Async context manager entry."""
        self._client = httpx.AsyncClient(timeout=self.timeout)
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb) -> None:
        """Async context manager exit."""
        if self._client:
            await self._client.aclose()
            self._client = None

    @property
    def client(self) -> httpx.AsyncClient:
        """Get the HTTP client."""
        if self._client is None:
            self._client = httpx.AsyncClient(timeout=self.timeout)
        return self._client

    async def _post(
        self,
        endpoint: str,
        data: dict[str, Any],
        retry: bool = True,
    ) -> dict[str, Any]:
        """Make a POST request with retry logic.

        Args:
            endpoint: API endpoint (info or exchange).
            data: Request payload.
            retry: Whether to retry on failure.

        Returns:
            Response JSON.

        Raises:
            APIError: On API errors.
        """
        url = f"{self.network.api_url}/{endpoint}"
        retries = 0
        last_error: Exception | None = None

        while retries <= self.max_retries:
            try:
                response = await self.client.post(url, json=data)

                # Handle rate limiting
                if response.status_code == 429:
                    if not retry or retries >= self.max_retries:
                        raise RateLimitError("Rate limit exceeded", status_code=429)
                    await self._wait_retry(retries)
                    retries += 1
                    continue

                # Handle server errors
                if response.status_code >= 500:
                    if not retry or retries >= self.max_retries:
                        raise APIError(
                            f"Server error: {response.status_code}",
                            status_code=response.status_code,
                        )
                    await self._wait_retry(retries)
                    retries += 1
                    continue

                # Parse response
                result = response.json()

                # Check for error response
                if isinstance(result, dict):
                    if result.get("status") == "err":
                        error_msg = result.get("response", "Unknown error")
                        self._handle_error(error_msg)

                return result

            except httpx.TimeoutException as e:
                last_error = e
                if not retry or retries >= self.max_retries:
                    raise APIError(f"Request timeout: {e}") from e
                await self._wait_retry(retries)
                retries += 1

            except httpx.RequestError as e:
                last_error = e
                if not retry or retries >= self.max_retries:
                    raise APIError(f"Request failed: {e}") from e
                await self._wait_retry(retries)
                retries += 1

        raise APIError(f"Max retries exceeded: {last_error}")

    async def _wait_retry(self, retry_count: int) -> None:
        """Wait before retrying with exponential backoff."""
        delay = self.retry_delay * (2**retry_count)
        await asyncio.sleep(delay)

    def _handle_error(self, error_msg: str) -> None:
        """Handle API error messages and raise appropriate exceptions."""
        error_lower = error_msg.lower()

        if "signature" in error_lower or "invalid sig" in error_lower:
            raise SignatureError(error_msg)
        if "margin" in error_lower or "insufficient" in error_lower:
            raise InsufficientMarginError(error_msg)
        if "not found" in error_lower or "unknown" in error_lower:
            raise AssetNotFoundError(error_msg)

        raise APIError(error_msg)

    @staticmethod
    def to_decimal(value: str | int | float | None) -> Decimal | None:
        """Convert a value to Decimal."""
        if value is None:
            return None
        return Decimal(str(value))
