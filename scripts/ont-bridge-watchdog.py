#!/usr/bin/env python3
"""
ONT Bridge Mode Watchdog & Auto-Remediator
==========================================

System Architecture & Network Design:
-------------------------------------
1. Reachability Behind NAT:
   When your ONT is connected to your router's WAN port, you do NOT need a physical
   cable swap to reach the ONT's management web interface (e.g. 192.168.1.1 or 192.168.100.1).
   You can establish Layer 3 reachability by configuring a WAN IP Alias + SNAT:
     - Assign a secondary IP alias on the router's physical WAN interface:
         ip addr add 192.168.1.254/24 dev eth0
     - Add an iptables/nftables MASQUERADE/SNAT rule for traffic destined to the ONT:
         iptables -t nat -I POSTROUTING -o eth0 -d 192.168.1.0/24 -j MASQUERADE

2. Failure Detection:
   Monitors outbound internet connectivity via multi-target ICMP pings (e.g. 1.1.1.1, 8.8.8.8).
   When internet drops, verifies if the ONT is responsive on its management IP.

3. Remediation:
   Logs into the ONT via HTTP API or Telnet, changes WAN mode back from 'Route' to 'Bridge',
   and triggers a WAN interface re-negotiation (e.g. PPPoE restart / DHCP release & renew)
   on your downstream router.
"""

import argparse
import logging
import os
import subprocess
import sys
import time
from typing import Dict, Optional, Tuple
import requests

# ---------------------------------------------------------------------------
# Default Configuration
# ---------------------------------------------------------------------------
DEFAULT_CONFIG = {
    "ont_ip": os.getenv("ONT_IP", "192.168.1.1"),
    "ont_user": os.getenv("ONT_USER", "admin"),
    "ont_pass": os.getenv("ONT_PASS", "admin"),
    "ont_driver": os.getenv("ONT_DRIVER", "generic_http"),  # 'generic_http', 'huawei', 'zte', 'telnet'
    "ping_targets": ["1.1.1.1", "8.8.8.8"],
    "check_interval_sec": 10,
    "fail_threshold": 3,
    "post_remediation_cooldown_sec": 45,
    "router_wan_interface": os.getenv("ROUTER_WAN_IFACE", "eth0"),
    "restart_wan_command": os.getenv("RESTART_WAN_CMD", ""),  # e.g., "ifup wan" or "killall -HUP pppd"
}

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)
logger = logging.getLogger("ont_watchdog")


# ---------------------------------------------------------------------------
# Network Prober
# ---------------------------------------------------------------------------
class NetworkProber:
    """Checks reachability to public internet targets and local ONT."""

    @staticmethod
    def ping(host: str, timeout: int = 2) -> bool:
        """Execute a single ICMP ping with a strict timeout."""
        try:
            res = subprocess.run(
                ["ping", "-c", "1", "-W", str(timeout), host],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            return res.returncode == 0
        except Exception as e:
            logger.debug(f"Ping execution error for {host}: {e}")
            return False

    @classmethod
    def check_internet(cls, targets: list) -> bool:
        """Return True if at least one internet target responds."""
        for target in targets:
            if cls.ping(target):
                return True
        return False

    @classmethod
    def check_ont_reachable(cls, ont_ip: str) -> bool:
        """Return True if the ONT management IP responds to ping or port 80/8080/23."""
        if cls.ping(ont_ip):
            return True
        # Fallback TCP check in case ICMP is firewalled on the ONT
        for port in [80, 8080, 443]:
            try:
                import socket
                with socket.create_connection((ont_ip, port), timeout=2):
                    return True
            except (socket.timeout, ConnectionRefusedError, OSError):
                continue
        return False


# ---------------------------------------------------------------------------
# ONT Remediation Drivers
# ---------------------------------------------------------------------------
class ONTDriverBase:
    """Abstract driver interface for ONT interactions."""

    def __init__(self, ip: str, username: str, password: str):
        self.ip = ip
        self.username = username
        self.password = password
        self.session = requests.Session()
        self.session.headers.update({
            "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36",
        })

    def switch_to_bridge(self) -> bool:
        raise NotImplementedError


class GenericHTTPDriver(ONTDriverBase):
    """
    Generic HTTP Driver:
    Customizable for ONTs with REST, CGI, or form-based interfaces.
    """

    def __init__(self, ip: str, username: str, password: str,
                 login_path: str = "/api/login",
                 wan_path: str = "/api/wan/config"):
        super().__init__(ip, username, password)
        self.login_url = f"http://{self.ip}{login_path}"
        self.wan_url = f"http://{self.ip}{wan_path}"

    def switch_to_bridge(self) -> bool:
        try:
            logger.info(f"Connecting to ONT Web API at {self.login_url}...")
            # 1. Login
            login_payload = {"username": self.username, "password": self.password}
            login_resp = self.session.post(self.login_url, json=login_payload, timeout=5)
            
            if login_resp.status_code not in [200, 302]:
                logger.warning(f"Login failed (HTTP {login_resp.status_code}). Trying form-encoded fallback...")
                login_resp = self.session.post(self.login_url, data=login_payload, timeout=5)
                if login_resp.status_code not in [200, 302]:
                    logger.error(f"Authentication failed: {login_resp.text[:200]}")
                    return False

            logger.info("Authentication successful. Sending Bridge WAN payload...")
            # 2. Set Bridge Mode Payload
            # Note: Parameter names vary by manufacturer. Adjust based on DevTools capture.
            bridge_payload = {
                "Mode": "Bridge",
                "ServiceType": "INTERNET",
                "LinkType": "IP_Bridged",
                "Enable": 1,
            }

            wan_resp = self.session.post(self.wan_url, json=bridge_payload, timeout=5)
            if wan_resp.status_code != 200:
                wan_resp = self.session.post(self.wan_url, data=bridge_payload, timeout=5)

            if wan_resp.status_code == 200:
                logger.info("Bridge mode configuration successfully pushed to ONT.")
                return True
            else:
                logger.error(f"Failed to push bridge configuration (HTTP {wan_resp.status_code})")
                return False

        except requests.exceptions.RequestException as e:
            logger.error(f"HTTP communication error with ONT: {e}")
            return False


class HuaweiONTDriver(ONTDriverBase):
    """
    Driver for Huawei HG8245 / EG8145 series GPON ONTs.
    Handles ASP token extraction and form POST to set.cgi / /asp/SetWan.asp.
    """

    def switch_to_bridge(self) -> bool:
        try:
            base_url = f"http://{self.ip}"
            logger.info(f"Attempting Huawei ONT login at {base_url}...")

            # Fetch root to get cookies / csrf token if present
            init_resp = self.session.get(f"{base_url}/", timeout=5)
            
            login_url = f"{base_url}/login.cgi"
            payload = {
                "UserName": self.username,
                "PassWord": self.password,
            }
            res = self.session.post(login_url, data=payload, timeout=5)
            if "login.asp" in res.text and "Password error" in res.text:
                logger.error("Huawei ONT: Invalid credentials.")
                return False

            logger.info("Huawei ONT authenticated. Applying bridge mode profile...")
            # Typical Huawei WAN config submission endpoint
            wan_url = f"{base_url}/set.cgi?x=InternetGatewayDevice.WANDevice.1.WANConnectionDevice.1.WANIPConnection.1"
            config_data = {
                "AddressingType": "DHCP",
                "ConnectionType": "IP_Bridged",
                "Mode": "Bridge",
            }
            set_res = self.session.post(wan_url, data=config_data, timeout=5)
            logger.info(f"Huawei ONT configuration response: {set_res.status_code}")
            return set_res.status_code == 200

        except requests.exceptions.RequestException as e:
            logger.error(f"Huawei ONT error: {e}")
            return False


class TelnetONTDriver(ONTDriverBase):
    """
    Driver for ONTs supporting Telnet/CLI management (port 23).
    Frequently the fastest and most stable method on ZTE/Huawei/Fiberhome ONTs.
    """

    def switch_to_bridge(self) -> bool:
        import telnetlib
        logger.info(f"Connecting to ONT Telnet at {self.ip}:23...")
        try:
            tn = telnetlib.Telnet(self.ip, 23, timeout=5)
            tn.read_until(b"Login: ", timeout=3)
            tn.write(self.username.encode("ascii") + b"\n")
            tn.read_until(b"Password: ", timeout=3)
            tn.write(self.password.encode("ascii") + b"\n")

            time.sleep(1)
            # Send common ONT CLI commands to set WAN to bridge
            # Example ZTE / Huawei generic commands:
            commands = [
                b"set wan mode bridge\n",
                b"save\n",
            ]
            for cmd in commands:
                tn.write(cmd)
                time.sleep(0.5)

            tn.write(b"exit\n")
            tn.close()
            logger.info("Telnet commands dispatched successfully.")
            return True
        except Exception as e:
            logger.error(f"Telnet connection error: {e}")
            return False


DRIVER_MAP = {
    "generic_http": GenericHTTPDriver,
    "huawei": HuaweiONTDriver,
    "telnet": TelnetONTDriver,
}


# ---------------------------------------------------------------------------
# Router WAN Manager (Helpers for reaching ONT and bouncing WAN)
# ---------------------------------------------------------------------------
class RouterWANManager:
    """Manages local router routes, WAN aliases, and interface bounces."""

    @staticmethod
    def ensure_wan_alias(wan_iface: str, ont_ip: str):
        """
        Ensures a secondary IP alias exists on the WAN interface so the local host/router
        can route packets to the ONT's management subnet.
        Example: If ONT is 192.168.1.1, alias is 192.168.1.254/24.
        """
        # Derive alias in same /24 subnet
        parts = ont_ip.split(".")
        alias_ip = f"{parts[0]}.{parts[1]}.{parts[2]}.254/24"
        logger.info(f"Setting up WAN IP Alias {alias_ip} on interface {wan_iface}...")
        
        # Check if alias already assigned
        check = subprocess.run(
            ["ip", "addr", "show", "dev", wan_iface],
            capture_output=True, text=True
        )
        if alias_ip.split('/')[0] not in check.stdout:
            add_res = subprocess.run(
                ["sudo", "ip", "addr", "add", alias_ip, "dev", wan_iface],
                capture_output=True, text=True
            )
            if add_res.returncode == 0:
                logger.info(f"Assigned {alias_ip} to {wan_iface}.")
            else:
                logger.warning(f"Failed to add WAN alias: {add_res.stderr.strip()}")

    @staticmethod
    def bounce_router_wan(restart_cmd: str):
        """Triggers PPPoE restart or DHCP renew on the downstream router."""
        if not restart_cmd:
            logger.info("No router WAN restart command configured. Skipping WAN bounce.")
            return

        logger.info(f"Executing router WAN bounce: '{restart_cmd}'...")
        try:
            res = subprocess.run(restart_cmd, shell=True, capture_output=True, text=True)
            if res.returncode == 0:
                logger.info("Router WAN bounced successfully.")
            else:
                logger.warning(f"Router WAN bounce failed: {res.stderr.strip()}")
        except Exception as e:
            logger.error(f"Error running WAN bounce command: {e}")


# ---------------------------------------------------------------------------
# Main Watchdog Loop
# ---------------------------------------------------------------------------
def run_watchdog(config: Dict, dry_run: bool = False, run_once: bool = False):
    logger.info("Starting ONT Bridge Watchdog...")
    logger.info(f"ONT Target: {config['ont_ip']} (Driver: {config['ont_driver']})")
    logger.info(f"Internet Targets: {config['ping_targets']}")

    driver_cls = DRIVER_MAP.get(config["ont_driver"], GenericHTTPDriver)
    driver = driver_cls(config["ont_ip"], config["ont_user"], config["ont_pass"])

    consecutive_failures = 0

    while True:
        internet_up = NetworkProber.check_internet(config["ping_targets"])

        if internet_up:
            if consecutive_failures > 0:
                logger.info("Internet connection restored.")
                consecutive_failures = 0
            logger.debug("Internet is UP.")
        else:
            consecutive_failures += 1
            logger.warning(
                f"Internet DOWN! (Consecutive failures: {consecutive_failures}/{config['fail_threshold']})"
            )

            if consecutive_failures >= config["fail_threshold"]:
                logger.warning("Failure threshold reached. Checking ONT reachability...")

                if NetworkProber.check_ont_reachable(config["ont_ip"]):
                    logger.info(f"ONT at {config['ont_ip']} is REACHABLE. Triggering Bridge Mode restore...")
                    if dry_run:
                        logger.info("[DRY RUN] Would execute switch_to_bridge() now.")
                        success = True
                    else:
                        success = driver.switch_to_bridge()

                    if success:
                        logger.info("Bridge command sent. Bouncing router WAN interface...")
                        if not dry_run:
                            RouterWANManager.bounce_router_wan(config["restart_wan_command"])

                        logger.info(f"Entering post-remediation cooldown ({config['post_remediation_cooldown_sec']}s)...")
                        time.sleep(config["post_remediation_cooldown_sec"])
                        consecutive_failures = 0
                    else:
                        logger.error("Failed to reconfigure ONT to Bridge mode. Retrying on next cycle.")
                else:
                    logger.error(
                        f"ONT at {config['ont_ip']} is NOT reachable! "
                        f"Ensure WAN IP Alias / NAT Masquerade rule is configured on your router."
                    )

        if run_once:
            break

        time.sleep(config["check_interval_sec"])


# ---------------------------------------------------------------------------
# CLI Argument Parser
# ---------------------------------------------------------------------------
def main():
    parser = argparse.ArgumentParser(description="ONT Bridge Mode Watchdog")
    parser.add_argument("--dry-run", action="store_true", help="Simulate actions without modifying ONT")
    parser.add_argument("--once", action="store_true", help="Run a single health check cycle and exit")
    parser.add_argument("--ont-ip", default=DEFAULT_CONFIG["ont_ip"], help="ONT Management IP")
    parser.add_argument("--driver", default=DEFAULT_CONFIG["ont_driver"], choices=DRIVER_MAP.keys(), help="ONT Driver")
    parser.add_argument("--setup-alias", action="store_true", help="Attempt to configure local WAN IP alias")
    parser.add_argument("--wan-iface", default=DEFAULT_CONFIG["router_wan_interface"], help="Router physical WAN iface")

    args = parser.parse_args()

    config = DEFAULT_CONFIG.copy()
    config["ont_ip"] = args.ont_ip
    config["ont_driver"] = args.driver
    config["router_wan_interface"] = args.wan_iface

    if args.setup_alias:
        RouterWANManager.ensure_wan_alias(config["router_wan_interface"], config["ont_ip"])

    run_watchdog(config, dry_run=args.dry_run, run_once=args.once)


if __name__ == "__main__":
    main()
