#!/usr/bin/env python3
"""
Huawei HS8546V5 & Multi-ONT Bridge Mode Watchdog (Docker Edition)
==================================================================
Monitors internet connectivity from inside Docker.
When internet drops (e.g., after ONT reboot/OMCI reset) and ONT is reachable,
automatically logs into Huawei HS8546V5 via BBSP API and reverts WAN to Bridge mode.
"""

import base64
import json
import logging
import os
import re
import subprocess
import sys
import time
from typing import Dict, List, Optional
import requests

# ---------------------------------------------------------------------------
# Configuration via Environment Variables
# ---------------------------------------------------------------------------
ONT_IP = os.getenv("ONT_IP", "192.168.100.1")
ONT_USER = os.getenv("ONT_USER", "telecomadmin")
ONT_PASS = os.getenv("ONT_PASS", "qi#9E!Bz@aWzdv#!TtfYuOjOSfh2r2Vj")
ONT_DRIVER = os.getenv("ONT_DRIVER", "huawei").lower()

PING_TARGETS = [t.strip() for t in os.getenv("PING_TARGETS", "1.1.1.1,8.8.8.8").split(",") if t.strip()]
CHECK_INTERVAL_SEC = int(os.getenv("CHECK_INTERVAL_SEC", "10"))
FAIL_THRESHOLD = int(os.getenv("FAIL_THRESHOLD", "3"))
COOLDOWN_SEC = int(os.getenv("COOLDOWN_SEC", "45"))
DRY_RUN = os.getenv("DRY_RUN", "false").lower() in ("true", "1", "yes")

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
    @staticmethod
    def ping(host: str, timeout: int = 2) -> bool:
        try:
            res = subprocess.run(
                ["ping", "-c", "1", "-W", str(timeout), host],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            return res.returncode == 0
        except Exception:
            return False

    @classmethod
    def check_internet(cls, targets: List[str]) -> bool:
        for target in targets:
            if cls.ping(target):
                return True
        return False

    @classmethod
    def check_ont_alive(cls, ont_ip: str) -> bool:
        if cls.ping(ont_ip):
            return True
        for port in [80, 8080, 443]:
            try:
                import socket
                with socket.create_connection((ont_ip, port), timeout=2):
                    return True
            except (socket.timeout, ConnectionRefusedError, OSError):
                continue
        return False


# ---------------------------------------------------------------------------
# Huawei HS8546V5 Driver (BBSP Platform)
# ---------------------------------------------------------------------------
class HuaweiBBSPDriver:
    """Specialized driver for Huawei HS8546V5 (BBSP complex.cgi architecture)."""

    def __init__(self, ip: str, user: str, password: str):
        self.ip = ip
        self.user = user
        self.password = password
        self.base_url = f"http://{self.ip}"
        self.session = requests.Session()
        self.session.headers.update({
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36",
            "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
            "Accept-Language": "en-US,en;q=0.9",
        })

    def login(self) -> bool:
        try:
            logger.info(f"[Huawei] Initiating authentication with {self.base_url}...")
            
            # 1. Fetch Nonce Token from /asp/GetRandCount.asp (strip UTF-8 BOM)
            rand_url = f"{self.base_url}/asp/GetRandCount.asp"
            r_rand = self.session.post(rand_url, timeout=4)
            nonce = r_rand.content.decode("utf-8-sig").strip()
            if not nonce:
                logger.error("[Huawei] Failed to acquire nonce token from /asp/GetRandCount.asp")
                return False

            logger.info(f"[Huawei] Nonce acquired: {nonce[:8]}...")

            # 2. Base64 encode password
            pw_b64 = base64.b64encode(self.password.encode("utf-8")).decode("utf-8")

            # 3. Post to /login.cgi
            login_url = f"{self.base_url}/login.cgi"
            login_data = {
                "UserName": self.user,
                "PassWord": pw_b64,
                "Language": "english",
                "x.X_HW_Token": nonce,
            }
            login_headers = {
                "Origin": self.base_url,
                "Referer": f"{self.base_url}/",
                "Cookie": "Cookie=body:Language:english:id=-1",
                "Content-Type": "application/x-www-form-urlencoded",
            }

            resp = self.session.post(login_url, data=login_data, headers=login_headers, timeout=5)
            
            # The Huawei server sets Cookie in Set-Cookie header: Cookie=sid=...:Language:english:id=1
            set_cookie = resp.headers.get("Set-Cookie", "")
            if "sid=" in set_cookie or "sid=" in self.session.cookies.get("Cookie", ""):
                logger.info("[Huawei] Authentication SUCCESS! Session established.")
                return True

            logger.error(f"[Huawei] Login failed. Status: {resp.status_code}")
            return False

        except requests.exceptions.RequestException as e:
            logger.error(f"[Huawei] Connection error during login: {e}")
            return False

    def switch_to_bridge(self) -> bool:
        if not self.login():
            return False

        try:
            logger.info("[Huawei] Fetching WAN management page to extract form token...")
            wan_url = f"{self.base_url}/html/bbsp/wan/wan.asp"
            wan_res = self.session.get(wan_url, timeout=5)
            
            if wan_res.status_code != 200:
                logger.error(f"[Huawei] Failed to fetch wan.asp (HTTP {wan_res.status_code})")
                return False

            # Extract token: id="hwonttoken" value="..." or name="onttoken"
            m = re.search(r'id=[\'\"]hwonttoken[\'\"][^>]+value=[\'\"]([a-f0-9]+)[\'\"]', wan_res.text)
            if not m:
                m = re.search(r'name=[\'\"]onttoken[\'\"][^>]+value=[\'\"]([a-f0-9]+)[\'\"]', wan_res.text)

            if not m:
                logger.error("[Huawei] Could not locate hwonttoken on wan.asp")
                return False

            form_token = m.group(1)
            logger.info(f"[Huawei] Form token acquired: {form_token[:8]}...")

            # Submit Bridge WAN configuration to complex.cgi
            submit_url = (
                f"{self.base_url}/html/bbsp/wan/complex.cgi"
                f"?y=InternetGatewayDevice.WANDevice.1.WANConnectionDevice.1.WANPPPConnection.1"
                f"&RequestFile=html/bbsp/wan/confirmwancfginfo.html"
            )

            payload = {
                "y.Enable": "1",
                "y.X_HW_IPv4Enable": "1",
                "y.X_HW_IPv6Enable": "0",
                "y.X_HW_IPv6MultiCastVLAN": "-1",
                "y.X_HW_SERVICELIST": "INTERNET",
                "y.X_HW_ExServiceList": "",
                "y.X_HW_VLAN": "10",
                "y.X_HW_PRI": "0",
                "y.X_HW_PriPolicy": "Specified",
                "y.X_HW_DefaultPri": "0",
                "y.ConnectionType": "PPPoE_Bridged",
                "y.X_HW_MultiCastVLAN": "4294967295",
                "y.X_HW_BindPhyPortInfo": "",
                "x.X_HW_Token": form_token,
            }

            submit_headers = {
                "Origin": self.base_url,
                "Referer": wan_url,
                "Content-Type": "application/x-www-form-urlencoded",
            }

            logger.info("[Huawei] Submitting Bridge WAN configuration to complex.cgi...")
            sub_res = self.session.post(submit_url, data=payload, headers=submit_headers, timeout=6)
            
            if sub_res.status_code in (200, 302):
                logger.info("[Huawei] ✅ BRIDGE MODE APPLIED SUCCESSFULLY!")
                return True
            else:
                logger.error(f"[Huawei] Submission failed with status {sub_res.status_code}")
                return False

        except requests.exceptions.RequestException as e:
            logger.error(f"[Huawei] Error during WAN configuration: {e}")
            return False


# ---------------------------------------------------------------------------
# Driver Registry
# ---------------------------------------------------------------------------
DRIVERS = {
    "huawei": HuaweiBBSPDriver,
}


# ---------------------------------------------------------------------------
# Main Watchdog Loop
# ---------------------------------------------------------------------------
def main():
    logger.info("==========================================")
    logger.info("   ONT Bridge Mode Watchdog (Docker)      ")
    logger.info("==========================================")
    logger.info(f"Target ONT IP  : {ONT_IP}")
    logger.info(f"Driver         : {ONT_DRIVER}")
    logger.info(f"User           : {ONT_USER}")
    logger.info(f"Ping Targets   : {PING_TARGETS}")
    logger.info(f"Check Interval : {CHECK_INTERVAL_SEC}s")
    logger.info(f"Dry Run Mode   : {DRY_RUN}")

    driver_cls = DRIVERS.get(ONT_DRIVER, HuaweiBBSPDriver)
    driver = driver_cls(ONT_IP, ONT_USER, ONT_PASS)

    consecutive_fails = 0

    while True:
        internet_up = NetworkProber.check_internet(PING_TARGETS)

        if internet_up:
            if consecutive_fails > 0:
                logger.info(">> 🌐 Internet connection is ONLINE and stable! <<")
                consecutive_fails = 0
            logger.debug("Internet is alive.")
        else:
            consecutive_fails += 1
            logger.warning(f"⚠️ Internet probe failed ({consecutive_fails}/{FAIL_THRESHOLD})")

            if consecutive_fails >= FAIL_THRESHOLD:
                logger.warning("Failure threshold reached! Verifying ONT reachability...")

                if NetworkProber.check_ont_alive(ONT_IP):
                    logger.info(f"ONT at {ONT_IP} is REACHABLE! Triggering Bridge Mode Remediation...")
                    if DRY_RUN:
                        logger.info("[DRY RUN] Would execute switch_to_bridge() now.")
                        success = True
                    else:
                        success = driver.switch_to_bridge()

                    if success:
                        logger.info(f"Configuration pushed. Entering cooldown for {COOLDOWN_SEC}s...")
                        time.sleep(COOLDOWN_SEC)
                        consecutive_fails = 0
                    else:
                        logger.error("Remediation attempt failed. Will retry on next failure cycle.")
                else:
                    logger.error(
                        f"ONT at {ONT_IP} is UNREACHABLE! "
                        f"Check OpenWrt WAN alias configuration."
                    )

        time.sleep(CHECK_INTERVAL_SEC)


if __name__ == "__main__":
    main()
