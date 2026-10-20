#!/usr/bin/env python3
"""
Huawei HS8546V5 Login & WAN Inspector Tool
===========================================
Tests authentication against your Huawei HS8546V5 ONT, extracts active sessions,
identifies CSRF tokens, and lists all configured WAN profiles.
"""

import base64
import re
import sys
import requests

ONT_IP = "192.168.100.1"
USERNAME = "telecomadmin"
PASSWORD = "qi#9E!Bz@aWzdv#!TtfYuOjOSfh2r2Vj"

BASE_URL = f"http://{ONT_IP}"

print("=" * 60)
print(f"🛰️  Testing Connection to Huawei HS8546V5 at {BASE_URL}")
print("=" * 60)

session = requests.Session()
session.headers.update({
    "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko)",
    "Referer": f"{BASE_URL}/",
})

# ---------------------------------------------------------------------------
# Step 1: Reachability Check
# ---------------------------------------------------------------------------
try:
    print(f"[*] Step 1: Probing {BASE_URL}...")
    resp = session.get(f"{BASE_URL}/", timeout=4)
    print(f"[+] Root page responded with HTTP {resp.status_code}")
except Exception as e:
    print(f"[-] Cannot reach ONT at {BASE_URL}: {e}")
    print("    Make sure OpenWrt WAN alias is configured (192.168.100.254 on physical WAN port).")
    sys.exit(1)

# ---------------------------------------------------------------------------
# Step 2: Fetch Anti-CSRF Token / Nonce (GetRandCount.asp)
# ---------------------------------------------------------------------------
token = ""
rand_url = f"{BASE_URL}/asp/GetRandCount.asp"
try:
    print("[*] Step 2: Fetching authentication token from /asp/GetRandCount.asp...")
    r_token = session.get(rand_url, timeout=3)
    if r_token.status_code == 200 and r_token.text.strip():
        token = r_token.text.strip()
        print(f"[+] Nonce token acquired: {token}")
except Exception as e:
    print(f"[!] Warning: Could not fetch GetRandCount: {e}")

# ---------------------------------------------------------------------------
# Step 3: Attempt Authentication
# ---------------------------------------------------------------------------
pass_b64 = base64.b64encode(PASSWORD.encode("utf-8")).decode("utf-8")

login_attempts = [
    # Variant A: Standard Huawei V5 (Base64 password + x.X_HW_Token)
    {
        "name": "Huawei V5 (Base64 + Token)",
        "url": f"{BASE_URL}/login.cgi",
        "data": {
            "UserName": USERNAME,
            "PassWord": pass_b64,
            "x.X_HW_Token": token,
        },
        "cookies": {"body": "Language:english:id=-1"},
    },
    # Variant B: Plaintext password + Token
    {
        "name": "Huawei V5 (Plaintext + Token)",
        "url": f"{BASE_URL}/login.cgi",
        "data": {
            "UserName": USERNAME,
            "PassWord": PASSWORD,
            "x.X_HW_Token": token,
        },
        "cookies": {"body": "Language:english:id=-1"},
    },
    # Variant C: Telkom / Operator customized form (lgName / encrypPass)
    {
        "name": "Huawei Operator Form (lgName/encrypPass)",
        "url": f"{BASE_URL}/login.cgi",
        "data": {
            "lgName": USERNAME,
            "encrypPass": PASSWORD,
        },
        "cookies": {},
    },
]

logged_in = False
for attempt in login_attempts:
    print(f"[*] Attempting login via: {attempt['name']}...")
    try:
        session.cookies.update(attempt["cookies"])
        res = session.post(attempt["url"], data=attempt["data"], timeout=5)
        
        # Check success indicators
        if "Password error" in res.text or "password wrong" in res.text.lower():
            print(f"[-] Credentials rejected for {attempt['name']}.")
            continue
        
        # Check for session cookie or redirect
        sid = session.cookies.get("sid") or session.cookies.get("SessionID")
        if sid or ("html/index" in res.text) or ("frame.asp" in res.text) or (res.status_code in [200, 302]):
            print(f"[+] SUCCESS! Authenticated via {attempt['name']}.")
            if sid:
                print(f"[+] Session Cookie (sid): {sid}")
            logged_in = True
            break
    except Exception as e:
        print(f"[-] Attempt error: {e}")

if not logged_in:
    print("[-] All automated login variants failed. Please capture cURL via browser DevTools.")
    sys.exit(1)

# ---------------------------------------------------------------------------
# Step 4: Inspect WAN Profiles and Configuration Page
# ---------------------------------------------------------------------------
print("\n[*] Step 4: Fetching WAN Configuration Page...")
wan_endpoints = [
    f"{BASE_URL}/html/network/wan.asp",
    f"{BASE_URL}/asp/GetWanList.asp",
    f"{BASE_URL}/html/bbsp/wan/wan.asp",
]

found_wan = False
for wan_url in wan_endpoints:
    try:
        w_res = session.get(wan_url, timeout=5)
        if w_res.status_code == 200 and len(w_res.text) > 200:
            print(f"[+] Found WAN page at: {wan_url} ({len(w_res.text)} bytes)")
            found_wan = True
            
            # Search for onttoken / security token
            onttoken_match = re.search(r'id=["\']?onttoken["\']?\s+value=["\']?(\w+)["\']?', w_res.text)
            if onttoken_match:
                print(f"[+] Active Form Token (onttoken): {onttoken_match.group(1)}")
            
            # Search for WAN Connection Names (e.g. 1_TR069_INTERNET_R_VID_...)
            wan_names = re.findall(r'(\d+_[A-Za-z0-9_]+_VID_\d+)', w_res.text)
            if wan_names:
                print(f"[+] Detected WAN Interfaces: {list(set(wan_names))}")
            else:
                # Look for generic connection entries
                conn_matches = re.findall(r'(WANConnectionDevice\.\d+\.WAN[A-Za-z]+\.\d+)', w_res.text)
                if conn_matches:
                    print(f"[+] Detected WAN TR-069 nodes: {list(set(conn_matches))}")
            break
    except Exception as e:
        continue

if not found_wan:
    print("[!] Could not auto-locate WAN page. The DevTools method below will reveal the exact URL.")

print("\n" + "=" * 60)
print("✅ Test Completed.")
print("=" * 60)
