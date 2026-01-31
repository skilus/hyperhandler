"""CLI interface for hlhandler."""

import asyncio
import getpass
import json
import sys
from decimal import Decimal
from pathlib import Path
from typing import Annotated

import typer
from rich.console import Console
from rich.table import Table

from hlhandler import __version__
from hlhandler.config import NETWORKS, get_config
from hlhandler.wallet import WalletManager

app = typer.Typer(
    name="hlhandler",
    help="Hyperliquid trading handler CLI",
    no_args_is_help=True,
)

config_app = typer.Typer(
    name="config",
    help="Configuration and wallet management",
    no_args_is_help=True,
)
app.add_typer(config_app, name="config")

vaults_app = typer.Typer(
    name="vaults",
    help="Vault operations",
    no_args_is_help=True,
)
app.add_typer(vaults_app, name="vaults")

console = Console()


# Common options
NetworkOption = Annotated[
    str,
    typer.Option("--network", "-n", help="Network (mainnet/testnet)", envvar="HL_NETWORK"),
]

VaultOption = Annotated[
    str | None,
    typer.Option("--vault", "-v", help="Vault address for vault trading"),
]


def get_wallet_and_signer(network: str):
    """Get wallet manager and create signer."""
    from hlhandler.signer import Signer

    manager = WalletManager(allow_prompt=True)
    key_result = manager.get_private_key(network)

    if not key_result:
        console.print(f"[red]No private key configured for {network}[/red]")
        console.print("Use 'hlhandler config set-key' or set HL_PRIVATE_KEY environment variable.")
        raise typer.Exit(1)

    is_mainnet = network == "mainnet"
    signer = Signer(key_result.key, is_mainnet=is_mainnet)
    return manager, signer


def run_async(coro):
    """Run an async coroutine."""
    return asyncio.run(coro)


def version_callback(value: bool) -> None:
    if value:
        console.print(f"hlhandler version {__version__}")
        raise typer.Exit()


@app.callback()
def main(
    version: Annotated[
        bool,
        typer.Option("--version", "-V", callback=version_callback, is_eager=True),
    ] = False,
) -> None:
    """Hyperliquid trading handler CLI."""
    pass


# =============================================================================
# Trading Commands
# =============================================================================


@app.command()
def exec(
    signal_file: Annotated[
        Path | None,
        typer.Option("--signal", "-s", help="Path to signal JSON file"),
    ] = None,
    network: NetworkOption = "mainnet",
    vault: VaultOption = None,
    dry_run: Annotated[
        bool,
        typer.Option("--dry-run", help="Validate only, don't execute"),
    ] = False,
) -> None:
    """Execute a trading signal."""
    from hlhandler.client import ExchangeClient, InfoClient
    from hlhandler.config import get_config
    from hlhandler.models import SignalValidator, TradingSignal, ValidationConfig
    from hlhandler.storage import get_storage

    # Read signal from file or stdin
    if signal_file:
        if not signal_file.exists():
            console.print(f"[red]Signal file not found: {signal_file}[/red]")
            raise typer.Exit(1)
        signal_data = json.loads(signal_file.read_text())
    else:
        if sys.stdin.isatty():
            console.print("[red]No signal file provided and stdin is empty[/red]")
            console.print("Usage: hlhandler exec --signal signal.json")
            console.print("   or: echo '{...}' | hlhandler exec")
            raise typer.Exit(1)
        signal_data = json.load(sys.stdin)

    # Parse and validate signal
    try:
        signal = TradingSignal(**signal_data)
    except Exception as e:
        console.print(f"[red]Invalid signal: {e}[/red]")
        raise typer.Exit(1)

    # Validate against config limits
    config = get_config()
    validator = SignalValidator(
        ValidationConfig(
            max_position_size_usd=Decimal(str(config.get("security", {}).get("max_position_size_usd", 10000))),
            max_leverage=config.get("security", {}).get("max_leverage", 20),
            require_stop_loss=config.get("security", {}).get("require_stop_loss", False),
        )
    )

    result = validator.validate(signal)
    if not result.valid:
        console.print("[red]Signal validation failed:[/red]")
        for error in result.errors:
            console.print(f"  - {error}")
        raise typer.Exit(1)

    for warning in result.warnings:
        console.print(f"[yellow]Warning: {warning}[/yellow]")

    if dry_run:
        console.print("[green]Signal validated successfully (dry run)[/green]")
        _print_signal_summary(signal)
        raise typer.Exit(0)

    # Execute the signal
    _, signer = get_wallet_and_signer(network)
    network_config = NETWORKS[network]
    storage = get_storage()

    # Save signal to storage
    signal_id = storage.save_signal(signal, network, validated=True, executed=False)

    async def execute():
        async with InfoClient(network_config) as info_client:
            async with ExchangeClient(network_config, signer) as exchange_client:
                # Get asset index
                asset_index = await info_client.get_asset_index(signal.pair)

                # Get current price for market orders
                current_price = None
                if signal.is_market:
                    current_price = await info_client.get_mid_price(signal.pair)

                # Set leverage
                console.print(f"Setting leverage to {signal.leverage}x...")
                await exchange_client.set_leverage(asset_index, signal.leverage, vault_address=vault)

                # Place orders
                console.print("Placing orders...")
                results = await exchange_client.place_order_from_signal(
                    signal=signal,
                    asset_index=asset_index,
                    current_price=current_price,
                    vault_address=vault,
                )

                return results

    try:
        with console.status("Executing signal..."):
            results = run_async(execute())

        storage.update_signal_executed(signal_id, True)

        # Save order results
        for i, result in enumerate(results):
            order_type = "entry" if i == 0 else ("sl" if i == 1 and signal.stop_loss else "tp")
            storage.save_order(
                signal_id=signal_id,
                network=network,
                pair=signal.pair,
                side=signal.side.value,
                order_type=order_type,
                size=signal.size,
                price=signal.entry_price,
                result=result,
                vault_address=vault,
            )

        # Print results
        success_count = sum(1 for r in results if r.success)
        console.print(f"\n[green]Executed {success_count}/{len(results)} orders successfully[/green]")

        for i, result in enumerate(results):
            order_type = "Entry" if i == 0 else ("Stop-Loss" if i == 1 and signal.stop_loss else "Take-Profit")
            if result.success:
                console.print(f"  {order_type}: Order ID {result.order_id}")
            else:
                console.print(f"  [red]{order_type}: Failed - {result.error}[/red]")

    except Exception as e:
        console.print(f"[red]Execution failed: {e}[/red]")
        raise typer.Exit(1)


@app.command()
def validate(
    signal_file: Annotated[
        Path,
        typer.Option("--signal", "-s", help="Path to signal JSON file"),
    ],
) -> None:
    """Validate a trading signal without executing."""
    from hlhandler.models import SignalValidator, TradingSignal

    if not signal_file.exists():
        console.print(f"[red]Signal file not found: {signal_file}[/red]")
        raise typer.Exit(1)

    try:
        signal_data = json.loads(signal_file.read_text())
        signal = TradingSignal(**signal_data)
    except Exception as e:
        console.print(f"[red]Invalid signal: {e}[/red]")
        raise typer.Exit(1)

    validator = SignalValidator()
    result = validator.validate(signal)

    if result.valid:
        console.print("[green]Signal is valid[/green]")
        _print_signal_summary(signal)
    else:
        console.print("[red]Signal validation failed:[/red]")
        for error in result.errors:
            console.print(f"  - {error}")
        raise typer.Exit(1)

    for warning in result.warnings:
        console.print(f"[yellow]Warning: {warning}[/yellow]")


@app.command()
def positions(
    network: NetworkOption = "mainnet",
    vault: VaultOption = None,
) -> None:
    """Show open positions."""
    from hlhandler.client import InfoClient

    _, signer = get_wallet_and_signer(network)
    network_config = NETWORKS[network]
    address = vault or signer.address

    async def fetch():
        async with InfoClient(network_config) as client:
            return await client.get_positions(address)

    with console.status("Fetching positions..."):
        pos_list = run_async(fetch())

    if not pos_list:
        console.print("[dim]No open positions[/dim]")
        return

    table = Table(title=f"Positions ({network})")
    table.add_column("Pair", style="cyan")
    table.add_column("Side", style="white")
    table.add_column("Size", style="white", justify="right")
    table.add_column("Entry", style="white", justify="right")
    table.add_column("Value", style="white", justify="right")
    table.add_column("PnL", justify="right")
    table.add_column("Leverage", style="yellow", justify="right")

    for pos in pos_list:
        side = "[green]LONG[/green]" if pos.is_long else "[red]SHORT[/red]"
        pnl_color = "green" if pos.unrealized_pnl >= 0 else "red"
        pnl = f"[{pnl_color}]{pos.unrealized_pnl:+.2f}[/{pnl_color}]"

        table.add_row(
            pos.coin,
            side,
            str(pos.abs_size),
            f"{pos.entry_price:.2f}",
            f"${pos.position_value:.2f}",
            pnl,
            f"{pos.leverage}x",
        )

    console.print(table)


@app.command()
def orders(
    network: NetworkOption = "mainnet",
    vault: VaultOption = None,
) -> None:
    """Show open orders."""
    from hlhandler.client import InfoClient

    _, signer = get_wallet_and_signer(network)
    network_config = NETWORKS[network]
    address = vault or signer.address

    async def fetch():
        async with InfoClient(network_config) as client:
            return await client.get_open_orders(address)

    with console.status("Fetching orders..."):
        order_list = run_async(fetch())

    if not order_list:
        console.print("[dim]No open orders[/dim]")
        return

    table = Table(title=f"Open Orders ({network})")
    table.add_column("ID", style="cyan")
    table.add_column("Pair", style="white")
    table.add_column("Side", style="white")
    table.add_column("Price", style="white", justify="right")
    table.add_column("Size", style="white", justify="right")

    for order in order_list:
        side = "[green]BUY[/green]" if order.is_buy else "[red]SELL[/red]"
        table.add_row(
            str(order.order_id),
            order.coin,
            side,
            f"{order.price:.2f}",
            str(order.size),
        )

    console.print(table)


@app.command()
def status(
    network: NetworkOption = "mainnet",
) -> None:
    """Show account status."""
    from hlhandler.client import InfoClient

    _, signer = get_wallet_and_signer(network)
    network_config = NETWORKS[network]

    async def fetch():
        async with InfoClient(network_config) as client:
            margin = await client.get_margin_summary(signer.address)
            positions = await client.get_positions(signer.address)
            orders = await client.get_open_orders(signer.address)
            return margin, positions, orders

    with console.status("Fetching account status..."):
        margin, positions, orders = run_async(fetch())

    console.print(f"\n[bold]Account Status ({network})[/bold]")
    console.print(f"Address: [cyan]{signer.address}[/cyan]")
    console.print()

    console.print("[bold]Margin Summary:[/bold]")
    console.print(f"  Account Value: ${Decimal(margin.get('accountValue', 0)):.2f}")
    console.print(f"  Margin Used: ${Decimal(margin.get('totalMarginUsed', 0)):.2f}")
    console.print(f"  Position Value: ${Decimal(margin.get('totalNtlPos', 0)):.2f}")
    console.print()

    console.print(f"[bold]Positions:[/bold] {len(positions)}")
    console.print(f"[bold]Open Orders:[/bold] {len(orders)}")


@app.command()
def cancel(
    order_id: Annotated[
        int | None,
        typer.Option("--order-id", "-o", help="Order ID to cancel"),
    ] = None,
    pair: Annotated[
        str | None,
        typer.Option("--pair", "-p", help="Cancel all orders for this pair"),
    ] = None,
    all_orders: Annotated[
        bool,
        typer.Option("--all", help="Cancel all open orders"),
    ] = False,
    network: NetworkOption = "mainnet",
    vault: VaultOption = None,
) -> None:
    """Cancel orders."""
    from hlhandler.client import ExchangeClient, InfoClient

    if not order_id and not pair and not all_orders:
        console.print("[red]Specify --order-id, --pair, or --all[/red]")
        raise typer.Exit(1)

    _, signer = get_wallet_and_signer(network)
    network_config = NETWORKS[network]
    address = vault or signer.address

    async def do_cancel():
        async with InfoClient(network_config) as info_client:
            async with ExchangeClient(network_config, signer) as exchange_client:
                orders = await info_client.get_open_orders(address)

                if not orders:
                    return 0

                cancelled = 0
                for order in orders:
                    should_cancel = False

                    if all_orders:
                        should_cancel = True
                    elif order_id and order.order_id == order_id:
                        should_cancel = True
                    elif pair and order.coin.upper() == pair.upper():
                        should_cancel = True

                    if should_cancel:
                        asset_index = await info_client.get_asset_index(order.coin)
                        if await exchange_client.cancel_order(asset_index, order.order_id, vault):
                            cancelled += 1

                return cancelled

    with console.status("Cancelling orders..."):
        count = run_async(do_cancel())

    if count > 0:
        console.print(f"[green]Cancelled {count} order(s)[/green]")
    else:
        console.print("[yellow]No orders cancelled[/yellow]")


@app.command()
def faucet(
    network: NetworkOption = "testnet",
) -> None:
    """Request testnet funds (testnet only)."""
    if network != "testnet":
        console.print("[red]Faucet is only available on testnet[/red]")
        raise typer.Exit(1)

    from hlhandler.client.base import BaseClient

    _, signer = get_wallet_and_signer(network)
    network_config = NETWORKS[network]

    async def request_faucet():
        async with BaseClient(network_config) as client:
            result = await client._post("info", {"type": "faucet", "user": signer.address})
            return result

    with console.status("Requesting testnet funds..."):
        try:
            result = run_async(request_faucet())
            if result.get("status") == "ok":
                console.print("[green]Faucet request successful![/green]")
            else:
                console.print(f"[yellow]Faucet response: {result}[/yellow]")
        except Exception as e:
            console.print(f"[red]Faucet request failed: {e}[/red]")


# =============================================================================
# Vault Commands
# =============================================================================


@vaults_app.command("list")
def vaults_list(
    network: NetworkOption = "mainnet",
    min_tvl: Annotated[
        float | None,
        typer.Option("--min-tvl", help="Minimum TVL filter"),
    ] = None,
    min_apr: Annotated[
        float | None,
        typer.Option("--min-apr", help="Minimum APR filter"),
    ] = None,
    limit: Annotated[
        int,
        typer.Option("--limit", "-l", help="Maximum number of vaults to show"),
    ] = 20,
) -> None:
    """List public vaults."""
    from hlhandler.client import VaultClient

    network_config = NETWORKS[network]

    async def fetch():
        async with VaultClient(network_config) as client:
            return await client.list_vaults(
                min_tvl=Decimal(str(min_tvl)) if min_tvl else None,
                min_apr=Decimal(str(min_apr)) if min_apr else None,
            )

    with console.status("Fetching vaults..."):
        vaults = run_async(fetch())

    if not vaults:
        console.print("[dim]No vaults found[/dim]")
        return

    table = Table(title=f"Public Vaults ({network})")
    table.add_column("Address", style="cyan", max_width=12)
    table.add_column("Name", style="white")
    table.add_column("TVL", style="green", justify="right")
    table.add_column("APR", style="yellow", justify="right")
    table.add_column("Followers", justify="right")
    table.add_column("Profit Share", justify="right")

    for vault in vaults[:limit]:
        table.add_row(
            f"{vault.address[:6]}...{vault.address[-4:]}",
            vault.name[:30],
            f"${vault.tvl:,.0f}",
            f"{vault.apr:.1f}%",
            str(vault.followers),
            f"{vault.profit_share:.0f}%",
        )

    console.print(table)


@vaults_app.command("info")
def vaults_info(
    vault_address: Annotated[
        str,
        typer.Argument(help="Vault address"),
    ],
    network: NetworkOption = "mainnet",
) -> None:
    """Show vault details."""
    from hlhandler.client import VaultClient

    network_config = NETWORKS[network]

    async def fetch():
        async with VaultClient(network_config) as client:
            return await client.get_vault_details(vault_address)

    with console.status("Fetching vault details..."):
        try:
            details = run_async(fetch())
        except Exception as e:
            console.print(f"[red]Error: {e}[/red]")
            raise typer.Exit(1)

    info = details.info
    console.print(f"\n[bold]{info.name}[/bold]")
    console.print(f"Address: [cyan]{info.address}[/cyan]")
    console.print(f"Leader: [cyan]{info.leader}[/cyan]")
    console.print()
    console.print(f"TVL: [green]${info.tvl:,.2f}[/green]")
    console.print(f"APR: [yellow]{info.apr:.1f}%[/yellow]")
    console.print(f"Profit Share: {info.profit_share}%")
    console.print(f"Lockup Period: {info.lockup_hours:.1f} hours")
    console.print(f"Followers: {info.followers}")
    console.print(f"Public: {'Yes' if info.is_public else 'No'}")


@vaults_app.command("deposit")
def vaults_deposit(
    vault_address: Annotated[
        str,
        typer.Option("--vault", "-v", help="Vault address"),
    ],
    amount: Annotated[
        float,
        typer.Option("--amount", "-a", help="Amount in USD"),
    ],
    network: NetworkOption = "mainnet",
) -> None:
    """Deposit to a vault."""
    from hlhandler.client import VaultClient

    _, signer = get_wallet_and_signer(network)
    network_config = NETWORKS[network]

    async def do_deposit():
        async with VaultClient(network_config, signer=signer) as client:
            return await client.deposit_to_vault(vault_address, Decimal(str(amount)))

    with console.status(f"Depositing ${amount} to vault..."):
        try:
            success = run_async(do_deposit())
            if success:
                console.print(f"[green]Deposited ${amount} to vault[/green]")
            else:
                console.print("[red]Deposit failed[/red]")
        except Exception as e:
            console.print(f"[red]Error: {e}[/red]")
            raise typer.Exit(1)


@vaults_app.command("withdraw")
def vaults_withdraw(
    vault_address: Annotated[
        str,
        typer.Option("--vault", "-v", help="Vault address"),
    ],
    shares: Annotated[
        float,
        typer.Option("--shares", "-s", help="Shares to withdraw (0-1)"),
    ],
    network: NetworkOption = "mainnet",
) -> None:
    """Withdraw from a vault."""
    from hlhandler.client import VaultClient, LockupPeriodError

    _, signer = get_wallet_and_signer(network)
    network_config = NETWORKS[network]

    async def do_withdraw():
        async with VaultClient(network_config, signer=signer) as client:
            return await client.withdraw_from_vault(vault_address, Decimal(str(shares)))

    with console.status(f"Withdrawing {shares*100:.1f}% from vault..."):
        try:
            success = run_async(do_withdraw())
            if success:
                console.print(f"[green]Withdrew {shares*100:.1f}% from vault[/green]")
            else:
                console.print("[red]Withdrawal failed[/red]")
        except LockupPeriodError:
            console.print("[red]Cannot withdraw during lockup period[/red]")
            raise typer.Exit(1)
        except Exception as e:
            console.print(f"[red]Error: {e}[/red]")
            raise typer.Exit(1)


@vaults_app.command("my-positions")
def vaults_my_positions(
    network: NetworkOption = "mainnet",
) -> None:
    """Show my positions in vaults."""
    from hlhandler.client import VaultClient

    _, signer = get_wallet_and_signer(network)
    network_config = NETWORKS[network]

    async def fetch():
        async with VaultClient(network_config, signer=signer) as client:
            return await client.get_my_vault_positions()

    with console.status("Fetching vault positions..."):
        positions = run_async(fetch())

    if not positions:
        console.print("[dim]No vault positions[/dim]")
        return

    table = Table(title=f"My Vault Positions ({network})")
    table.add_column("Vault", style="cyan")
    table.add_column("Name", style="white")
    table.add_column("Deposited", style="white", justify="right")
    table.add_column("Current", style="white", justify="right")
    table.add_column("PnL", justify="right")
    table.add_column("PnL %", justify="right")

    for pos in positions:
        pnl_color = "green" if pos.pnl >= 0 else "red"
        table.add_row(
            f"{pos.vault[:6]}...{pos.vault[-4:]}",
            pos.vault_name[:20],
            f"${pos.deposited:.2f}",
            f"${pos.current_value:.2f}",
            f"[{pnl_color}]{pos.pnl:+.2f}[/{pnl_color}]",
            f"[{pnl_color}]{pos.pnl_percent:+.1f}%[/{pnl_color}]",
        )

    console.print(table)


# =============================================================================
# Config Commands
# =============================================================================


@config_app.command("set-key")
def set_key(
    network: Annotated[
        str,
        typer.Option("--network", "-n", help="Network name (mainnet/testnet)"),
    ] = "mainnet",
) -> None:
    """Save a private key to the system keyring."""
    if network not in NETWORKS:
        console.print(f"[red]Unknown network: {network}[/red]")
        console.print(f"Available networks: {', '.join(NETWORKS.keys())}")
        raise typer.Exit(1)

    try:
        key = getpass.getpass(f"Enter private key for {network}: ")
        if not key:
            console.print("[yellow]No key provided, operation cancelled.[/yellow]")
            raise typer.Exit(0)

        manager = WalletManager(allow_prompt=False)
        manager.save_to_keyring(network, key)

        address = manager.get_address(network)
        console.print(f"[green]Key saved to keyring for {network}[/green]")
        console.print(f"Address: {address}")

    except ValueError as e:
        console.print(f"[red]Invalid key: {e}[/red]")
        raise typer.Exit(1)
    except RuntimeError as e:
        console.print(f"[red]Keyring error: {e}[/red]")
        raise typer.Exit(1)


@config_app.command("remove-key")
def remove_key(
    network: Annotated[
        str,
        typer.Option("--network", "-n", help="Network name (mainnet/testnet)"),
    ] = "mainnet",
) -> None:
    """Remove a private key from the system keyring."""
    if network not in NETWORKS:
        console.print(f"[red]Unknown network: {network}[/red]")
        raise typer.Exit(1)

    manager = WalletManager(allow_prompt=False)
    if manager.remove_from_keyring(network):
        console.print(f"[green]Key removed from keyring for {network}[/green]")
    else:
        console.print(f"[yellow]No key found in keyring for {network}[/yellow]")


@config_app.command("show-address")
def show_address(
    network: Annotated[
        str | None,
        typer.Option("--network", "-n", help="Network name (mainnet/testnet)"),
    ] = None,
) -> None:
    """Show wallet addresses for configured keys."""
    networks_to_check = [network] if network else list(NETWORKS.keys())
    manager = WalletManager(allow_prompt=False)

    table = Table(title="Wallet Addresses")
    table.add_column("Network", style="cyan")
    table.add_column("Address", style="green")
    table.add_column("Source", style="yellow")

    found_any = False
    for net in networks_to_check:
        if net not in NETWORKS:
            console.print(f"[red]Unknown network: {net}[/red]")
            continue

        result = manager.get_private_key(net)
        if result:
            table.add_row(net, result.address, result.provider)
            found_any = True
        else:
            table.add_row(net, "[dim]not configured[/dim]", "-")

    console.print(table)

    if not found_any:
        console.print("\n[yellow]No keys configured. Use 'hlhandler config set-key' or set HL_PRIVATE_KEY environment variable.[/yellow]")


@config_app.command("check")
def check_config(
    network: Annotated[
        str | None,
        typer.Option("--network", "-n", help="Network name (mainnet/testnet)"),
    ] = None,
) -> None:
    """Check configuration and provider status."""
    config = get_config()
    manager = WalletManager(allow_prompt=False)

    console.print(f"[bold]Current network:[/bold] {config.network}")
    console.print(f"[bold]Config file:[/bold] {config.config_path}")
    console.print()

    table = Table(title="Networks")
    table.add_column("Network", style="cyan")
    table.add_column("API URL", style="white")

    for name, net_config in NETWORKS.items():
        table.add_row(name, net_config.api_url)

    console.print(table)
    console.print()

    networks_to_check = [network] if network else list(NETWORKS.keys())

    for net in networks_to_check:
        if net not in NETWORKS:
            continue

        console.print(f"[bold]Provider status for {net}:[/bold]")
        status = manager.check_providers(net)

        provider_table = Table(show_header=True)
        provider_table.add_column("Provider", style="cyan")
        provider_table.add_column("Available", style="white")
        provider_table.add_column("Has Key", style="white")

        for provider_name, info in status.items():
            available = "[green]yes[/green]" if info["available"] else "[red]no[/red]"
            has_key = "[green]yes[/green]" if info["has_key"] else "[dim]no[/dim]"
            provider_table.add_row(provider_name, available, has_key)

        console.print(provider_table)

        result = manager.get_private_key(net)
        if result:
            console.print(f"  Active key from: [yellow]{result.provider}[/yellow]")
            console.print(f"  Address: [green]{result.address}[/green]")
        console.print()


# =============================================================================
# Helper Functions
# =============================================================================


def _print_signal_summary(signal) -> None:
    """Print a summary of a trading signal."""
    console.print()
    console.print(f"  Pair: [cyan]{signal.pair}[/cyan]")
    console.print(f"  Side: {'[green]LONG[/green]' if signal.is_buy else '[red]SHORT[/red]'}")
    console.print(f"  Type: {signal.order_type.value}")
    console.print(f"  Size: {signal.size}")
    console.print(f"  Leverage: {signal.leverage}x")
    if signal.entry_price:
        console.print(f"  Entry Price: {signal.entry_price}")
    if signal.stop_loss:
        console.print(f"  Stop Loss: {signal.stop_loss}")
    if signal.take_profit:
        console.print(f"  Take Profit: {signal.take_profit}")


if __name__ == "__main__":
    app()
