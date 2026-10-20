#!/usr/bin/env python3
"""
Scan and rank 10,000 high-value .ai.id domain names across 3, 4, and 5-letter words.
Backed by SQLite for persistent caching and resuming.
"""

import socket
import re
import time
import json
import sqlite3
import csv
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

DB_PATH = '/home/agent/orca/learnings/ai_id_10k.db'
CSV_PATH = '/home/agent/orca/learnings/ai_id_10k_domains.csv'
JSON_PATH = '/home/agent/orca/learnings/ai_id_10k_summary.json'
REPORT_PATH = '/home/agent/.gemini/antigravity-cli/brain/4bc821ac-a484-44db-ae98-2b33d3c2d69c/ai_id_10k_domain_audit_report.md'

def init_db():
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    cur.execute('''
        CREATE TABLE IF NOT EXISTS domains (
            domain TEXT PRIMARY KEY,
            word TEXT,
            length INTEGER,
            status TEXT,
            registrar TEXT,
            created TEXT,
            expires TEXT,
            score INTEGER,
            tier TEXT,
            category TEXT,
            freq_rank INTEGER
        )
    ''')
    conn.commit()
    return conn

def load_words():
    print("Loading vocabulary and frequency lists...")
    # Load 50k frequency list
    url_50k = 'https://raw.githubusercontent.com/hermitdave/FrequencyWords/master/content/2018/en/en_50k.txt'
    req = urllib.request.Request(url_50k, headers={'User-Agent': 'Mozilla/5.0'})
    with urllib.request.urlopen(req, timeout=15) as resp:
        lines = resp.read().decode('utf-8').splitlines()

    freq_rank = {}
    freq_words = []
    for rank, line in enumerate(lines):
        parts = line.strip().split()
        if not parts:
            continue
        w = parts[0].lower()
        if w.isalpha():
            if w not in freq_rank:
                freq_rank[w] = rank
                freq_words.append(w)

    # Elite AI, Tech, Cloud & Fintech domain targets
    tech_ai_elite = [
        # AI & ML
        'gpt', 'llm', 'rag', 'nlp', 'agi', 'ann', 'moe', 'vit', 'bert', 'lora', 'dpo', 'sft',
        'agent', 'model', 'cloud', 'smart', 'super', 'scale', 'stack', 'voice', 'video', 'logic',
        'media', 'cyber', 'robot', 'trust', 'brand', 'token', 'chain', 'hyper', 'alpha', 'delta',
        'nexus', 'pulse', 'spark', 'orbit', 'prime', 'vault', 'graph', 'tensor', 'neural', 'matrix',
        'vector', 'prompt', 'tuning', 'vision', 'speech', 'search', 'answer', 'server', 'client', 'engine',
        'coder', 'codec', 'miner', 'layer', 'nodes', 'edges', 'flows', 'datas', 'bytes', 'llama',
        'mistral', 'claude', 'gemini', 'groq', 'cohere', 'modal', 'flux', 'sdxl', 'synth', 'align',
        # Dev, Infra, Web, Security
        'code', 'data', 'tech', 'chat', 'flow', 'mind', 'byte', 'open', 'deep', 'meta', 'fast',
        'node', 'core', 'base', 'team', 'fund', 'coin', 'bank', 'shop', 'work', 'edge', 'fire',
        'star', 'wave', 'card', 'user', 'auth', 'rust', 'java', 'ruby', 'perl', 'bash', 'cron',
        'grpc', 'rest', 'crud', 'wasm', 'llvm', 'cuda', 'json', 'yaml', 'toml', 'html', 'http',
        'grep', 'diff', 'push', 'pull', 'fork', 'init', 'main', 'head', 'tail', 'root', 'sudo',
        'ping', 'port', 'cert', 'hash', 'salt', 'leak', 'spam', 'hack', 'scan', 'test', 'auto',
        'link', 'host', 'free', 'good', 'cool', 'real', 'true', 'next', 'plus', 'peer', 'play',
        'live', 'game', 'draw', 'post', 'feed', 'read', 'edit', 'send', 'make', 'save', 'look',
        'find', 'seek', 'path', 'road', 'city', 'home', 'room', 'park', 'farm', 'food', 'cafe',
        'beer', 'wine', 'cake', 'fish', 'meat', 'rice', 'corn', 'seed', 'tree', 'leaf', 'gold',
        'iron', 'zinc', 'silk', 'wool', 'text', 'font', 'icon', 'logo', 'view', 'page', 'site',
        'blog', 'news', 'cast', 'song', 'film', 'ring', 'bell', 'door', 'gate', 'lock', 'safe',
        'wire', 'cord', 'pipe', 'pump', 'fuel', 'heat', 'cool', 'cold', 'warm', 'dark', 'glow',
        'beam', 'moon', 'wind', 'rain', 'snow', 'wave', 'tide', 'surf', 'sail', 'ship', 'boat',
        'helm', 'dock', 'deck', 'crew', 'boss', 'lead', 'king', 'lord', 'hero', 'idol', 'muse',
        'guru', 'sage', 'monk', 'apex', 'zero', 'unit', 'seed', 'mesh', 'grid', 'poly', 'mono',
        'flux', 'warp', 'dash', 'zoom', 'sync', 'loop', 'echo', 'ping', 'pong', 'tick', 'tock',
        'drop', 'plug', 'hook', 'pipe', 'vent', 'duct', 'gate', 'slot', 'rack', 'case', 'tray',
        # Fintech, Business, Growth
        'pay', 'fin', 'biz', 'pro', 'buy', 'bid', 'ask', 'fee', 'tip', 'val', 'cap', 'tax',
        'sub', 'ceo', 'cto', 'cfo', 'crm', 'erp', 'seo', 'sem', 'ads', 'vip', 'max', 'top',
        'deal', 'sale', 'loan', 'cash', 'debt', 'gain', 'risk', 'bull', 'bear', 'safe', 'fund',
        'swap', 'pool', 'mint', 'burn', 'vest', 'earn', 'save', 'lend', 'bill', 'rate', 'cost',
        'seed', 'hire', 'jobs', 'work', 'meet', 'talk', 'call', 'sync', 'team', 'crew', 'firm'
    ]

    seen = set()
    curated = []

    def add(w):
        w = w.strip().lower()
        if 3 <= len(w) <= 5 and w.isalpha() and w not in seen:
            seen.add(w)
            curated.append(w)

    for w in tech_ai_elite:
        add(w)

    for w in freq_words:
        if 3 <= len(w) <= 5:
            add(w)
            if len(curated) >= 10000:
                break

    words_10k = curated[:10000]
    print(f"Curated {len(words_10k):,} words (Len 3: {sum(1 for w in words_10k if len(w)==3)}, Len 4: {sum(1 for w in words_10k if len(w)==4)}, Len 5: {sum(1 for w in words_10k if len(w)==5)}).")
    return words_10k, freq_rank, set(tech_ai_elite)

def query_whois_safe(domain, max_retries=4):
    for attempt in range(max_retries):
        try:
            s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            s.settimeout(5)
            s.connect(('whois.id', 43))
            s.sendall(f"{domain}\r\n".encode('utf-8'))
            res = b''
            while True:
                chunk = s.recv(4096)
                if not chunk:
                    break
                res += chunk
            s.close()
            text = res.decode('utf-8', errors='ignore')

            if 'Too many requests' in text:
                time.sleep(2.0 + attempt * 1.5)
                continue

            if 'DOMAIN NOT FOUND' in text:
                return {'status': 'AVAILABLE', 'registrar': '', 'created': '', 'expires': ''}
            elif 'Domain Name:' in text:
                rm = re.search(r'Registrar:\s*(.+)', text)
                cm = re.search(r'Creation Date:\s*(.+)', text)
                em = re.search(r'Registry Expiry Date:\s*(.+)', text)
                return {
                    'status': 'REGISTERED',
                    'registrar': rm.group(1).strip() if rm else 'Unknown',
                    'created': cm.group(1).strip() if cm else 'Unknown',
                    'expires': em.group(1).strip() if em else 'Unknown'
                }
            else:
                time.sleep(0.5)
        except Exception:
            time.sleep(0.5 * (attempt + 1))

    return {'status': 'ERROR', 'registrar': '', 'created': '', 'expires': ''}

def categorize(w, is_tech):
    l = len(w)
    if is_tech or w in ['agent', 'model', 'cloud', 'smart', 'scale', 'stack', 'voice', 'logic', 'cyber', 'robot', 'token', 'chain', 'tensor', 'neural', 'matrix', 'vector', 'prompt', 'tuning', 'vision', 'speech', 'engine', 'llama', 'mistral', 'claude', 'gemini', 'groq', 'cohere', 'modal', 'flux', 'sdxl', 'gpt', 'llm', 'rag', 'nlp', 'agi', 'ann', 'moe', 'vit', 'bert', 'lora']:
        return 'AI & Core Tech'
    elif w in ['code', 'data', 'tech', 'byte', 'rust', 'java', 'ruby', 'bash', 'grpc', 'rest', 'crud', 'wasm', 'llvm', 'cuda', 'json', 'yaml', 'toml', 'html', 'http', 'grep', 'diff', 'push', 'pull', 'fork', 'init', 'root', 'sudo', 'ping', 'port', 'cert', 'hash', 'salt', 'node', 'core', 'base', 'sync', 'echo', 'loop', 'grid', 'poly', 'mono', 'mesh', 'warp', 'flux', 'pipe', 'slot', 'rack']:
        return 'Dev & Cloud Infra'
    elif w in ['pay', 'fin', 'biz', 'pro', 'buy', 'bid', 'ask', 'fee', 'tip', 'val', 'cap', 'tax', 'deal', 'sale', 'loan', 'cash', 'debt', 'gain', 'risk', 'bull', 'bear', 'fund', 'swap', 'pool', 'mint', 'burn', 'vest', 'earn', 'save', 'lend', 'bill', 'rate', 'cost', 'coin', 'bank', 'shop', 'card', 'vip', 'max', 'top']:
        return 'Fintech & Commerce'
    elif w in ['run', 'fly', 'get', 'set', 'try', 'see', 'hit', 'cut', 'tap', 'pop', 'let', 'put', 'say', 'eat', 'fit', 'fix', 'go', 'use', 'add', 'ask', 'opt', 'end', 'out', 'make', 'send', 'read', 'edit', 'save', 'look', 'find', 'seek', 'play', 'live', 'draw', 'post', 'feed', 'call', 'talk', 'meet']:
        return 'Action Verbs & CTAs'
    elif w in ['sun', 'sea', 'sky', 'air', 'ice', 'hot', 'red', 'cat', 'dog', 'fox', 'car', 'day', 'eye', 'way', 'joy', 'zen', 'art', 'bio', 'law', 'med', 'eco', 'geo', 'star', 'fire', 'wave', 'wind', 'moon', 'rain', 'snow', 'gold', 'iron', 'silk', 'hero', 'king', 'lord', 'guru', 'sage']:
        return 'Premium Brandables'
    else:
        return 'General High-Frequency English'

def score_domain(w, category, freq_rank_val):
    score = 0
    l = len(w)

    # 1. Length scarcity
    if l == 3:
        score += 35
    elif l == 4:
        score += 25
    elif l == 5:
        score += 15

    # 2. Category value
    if category == 'AI & Core Tech':
        score += 35
    elif category == 'Fintech & Commerce':
        score += 30
    elif category == 'Dev & Cloud Infra':
        score += 28
    elif category == 'Premium Brandables':
        score += 25
    elif category == 'Action Verbs & CTAs':
        score += 22
    else:
        score += 15

    # 3. Frequency rank
    if freq_rank_val < 500:
        score += 20
    elif freq_rank_val < 2000:
        score += 16
    elif freq_rank_val < 5000:
        score += 12
    elif freq_rank_val < 10000:
        score += 8
    else:
        score += 4

    # 4. Phonetic / CVC punch
    vowels = set('aeiouy')
    if l == 3 and w[0] not in vowels and w[1] in vowels and w[2] not in vowels:
        score += 10
    elif l == 4 and w[0] not in vowels and w[1] in vowels and w[2] not in vowels and w[3] in vowels:
        score += 8  # CVCV like 'data', 'code', 'meta', 'java'
    elif l == 4 and w[0] not in vowels and w[1] in vowels and w[2] in vowels and w[3] not in vowels:
        score += 8  # CVVC like 'team', 'coin', 'deep', 'feed'
    else:
        score += 5

    return min(score, 100)

def get_tier(score):
    if score >= 85:
        return 'Tier 1: Ultra-Premium'
    elif score >= 70:
        return 'Tier 2: High Value'
    elif score >= 55:
        return 'Tier 3: Good Brandable'
    elif score >= 40:
        return 'Tier 4: Standard'
    else:
        return 'Tier 5: Niche'

def main():
    conn = init_db()
    cur = conn.cursor()

    words_10k, freq_rank, tech_elite_set = load_words()

    # Pre-populate DB with previously verified 1,000 domains if available
    try:
        with open('/home/agent/orca/learnings/ai_id_1000_domains.json') as f:
            prev_data = json.load(f)
            prev_results = prev_data['results']
            print(f"Importing {len(prev_results)} previously verified domains from 1k audit...")
            for r in prev_results:
                cur.execute('''
                    INSERT OR REPLACE INTO domains 
                    (domain, word, length, status, registrar, created, expires, score, tier, category, freq_rank)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ''', (
                    r['domain'], r['word'], len(r['word']), r['status'],
                    r['registrar'], r['created'], r['expires'],
                    r['score'], r['tier'], r['category'], r['freq_rank']
                ))
            conn.commit()
    except Exception as e:
        print("Note: Could not load 1k cache:", e)

    # Check which domains need WHOIS query
    cur.execute("SELECT domain, status FROM domains")
    cached = {row[0]: row[1] for row in cur.fetchall()}

    to_query = []
    for w in words_10k:
        d = f"{w}.ai.id"
        if d not in cached or cached[d] not in ('AVAILABLE', 'REGISTERED'):
            to_query.append(w)

    print(f"Total domains to scan: {len(to_query):,} ({len(cached):,} already resolved).")

    if to_query:
        print("Starting threaded WHOIS scan with rate-limiting & auto-retry...")
        t_start = time.time()
        completed = 0
        batch = []

        with ThreadPoolExecutor(max_workers=5) as executor:
            future_to_word = {
                executor.submit(query_whois_safe, f"{w}.ai.id"): w for w in to_query
            }
            for future in as_completed(future_to_word):
                w = future_to_word[future]
                d = f"{w}.ai.id"
                try:
                    res = future.result()
                except Exception:
                    res = {'status': 'ERROR', 'registrar': '', 'created': '', 'expires': ''}

                category = categorize(w, w in tech_elite_set)
                frank = freq_rank.get(w, 99999)
                score = score_domain(w, category, frank)
                tier = get_tier(score)

                batch.append((
                    d, w, len(w), res['status'], res['registrar'], res['created'], res['expires'],
                    score, tier, category, frank
                ))
                completed += 1

                if len(batch) >= 100:
                    cur.executemany('''
                        INSERT OR REPLACE INTO domains 
                        (domain, word, length, status, registrar, created, expires, score, tier, category, freq_rank)
                        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    ''', batch)
                    conn.commit()
                    batch = []

                if completed % 500 == 0:
                    elapsed = time.time() - t_start
                    qps = completed / elapsed if elapsed > 0 else 0
                    print(f"Progress: {completed:,}/{len(to_query):,} scanned ({elapsed:.1f}s, {qps:.1f} qps)...")

            if batch:
                cur.executemany('''
                    INSERT OR REPLACE INTO domains 
                    (domain, word, length, status, registrar, created, expires, score, tier, category, freq_rank)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ''', batch)
                conn.commit()

        # Handle any remaining ERROR status domains
        cur.execute("SELECT domain, word FROM domains WHERE status = 'ERROR'")
        err_rows = cur.fetchall()
        if err_rows:
            print(f"Retrying {len(err_rows)} error records...")
            for d, w in err_rows:
                res = query_whois_safe(d, max_retries=5)
                cur.execute("UPDATE domains SET status=?, registrar=?, created=?, expires=? WHERE domain=?",
                            (res['status'], res['registrar'], res['created'], res['expires'], d))
                time.sleep(0.05)
            conn.commit()

    # Final query to build outputs
    cur.execute('''
        SELECT domain, word, length, status, score, tier, category, registrar, created, expires, freq_rank
        FROM domains
        ORDER BY score DESC, length ASC, freq_rank ASC, word ASC
    ''')
    all_rows = cur.fetchall()

    print(f"\nScan complete! Total records in DB: {len(all_rows):,}")

    available_rows = [r for r in all_rows if r[3] == 'AVAILABLE']
    registered_rows = [r for r in all_rows if r[3] == 'REGISTERED']
    error_rows = [r for r in all_rows if r[3] not in ('AVAILABLE', 'REGISTERED')]

    print(f"Available: {len(available_rows):,} ({len(available_rows)/len(all_rows)*100:.1f}%)")
    print(f"Registered: {len(registered_rows):,} ({len(registered_rows)/len(all_rows)*100:.1f}%)")
    print(f"Errors: {len(error_rows):,}")

    # Write CSV
    print(f"Exporting CSV to {CSV_PATH}...")
    with open(CSV_PATH, 'w', newline='', encoding='utf-8') as f:
        writer = csv.writer(f)
        writer.writerow(['rank', 'domain', 'word', 'length', 'status', 'score', 'tier', 'category', 'registrar', 'created', 'expires'])
        for idx, r in enumerate(all_rows, start=1):
            writer.writerow([idx, r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7], r[8], r[9]])

    # Write Summary JSON
    print(f"Exporting JSON summary to {JSON_PATH}...")
    with open(JSON_PATH, 'w', encoding='utf-8') as f:
        json.dump({
            'meta': {
                'scan_timestamp': time.strftime('%Y-%m-%d %H:%M:%S UTC', time.gmtime()),
                'total_scanned': len(all_rows),
                'available_count': len(available_rows),
                'registered_count': len(registered_rows),
                'availability_pct': round(len(available_rows)/len(all_rows)*100, 2),
                'by_length': {
                    '3': sum(1 for r in all_rows if r[2] == 3),
                    '4': sum(1 for r in all_rows if r[2] == 4),
                    '5': sum(1 for r in all_rows if r[2] == 5)
                }
            },
            'top_50_available': [
                {
                    'rank': idx, 'domain': r[0], 'word': r[1], 'length': r[2],
                    'score': r[4], 'tier': r[5], 'category': r[6]
                }
                for idx, r in enumerate(available_rows[:50], start=1)
            ],
            'top_30_registered': [
                {
                    'domain': r[0], 'word': r[1], 'score': r[4], 'registrar': r[7], 'created': r[8]
                }
                for r in registered_rows[:30]
            ]
        }, f, indent=2)

    conn.close()
    print("Done database export!")

if __name__ == '__main__':
    main()
