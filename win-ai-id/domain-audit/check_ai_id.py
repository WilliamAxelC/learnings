#!/usr/bin/env python3
"""
Check .ai.id domain availability for 1,000 curated 3-letter words and acronyms.
PANDI officially opened General Availability for .ai.id on October 5, 2026.
"""

import socket
import re
import time
import json
import csv
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

def get_wordlists():
    print("Loading wordlists...")
    # 1. Scrabble official 3-letter words
    scrabble_url = 'https://raw.githubusercontent.com/raun/Scrabble/master/words.txt'
    with urllib.request.urlopen(scrabble_url, timeout=10) as resp:
        scrabble = set(w.strip().lower() for w in resp.read().decode('utf-8').splitlines() if len(w.strip()) == 3 and w.strip().isalpha())

    # 2. Google 20k frequency list
    freq_url = 'https://raw.githubusercontent.com/first20hours/google-10000-english/master/20k.txt'
    req = urllib.request.Request(freq_url, headers={'User-Agent': 'Mozilla/5.0'})
    with urllib.request.urlopen(req, timeout=10) as resp:
        freq_raw = [w.strip().lower() for w in resp.read().decode('utf-8').splitlines() if len(w.strip()) == 3 and w.strip().isalpha()]

    freq_rank = {}
    for rank, w in enumerate(freq_raw):
        if w not in freq_rank:
            freq_rank[w] = rank

    # 3. High-priority Tech, AI, Cloud, Dev, Data, Cyber & Business terms
    ai_tech_terms = [
        # AI & ML Core
        'gpt', 'llm', 'rag', 'nlp', 'agi', 'ann', 'rnn', 'cnn', 'knn', 'svm',
        'gen', 'bot', 'sim', 'cog', 'neu', 'vec', 'emb', 'fit', 'hub', 'lab',
        'mod', 'vis', 'dat', 'eva', 'opt', 'act', 'ctx', 'syn', 'mem', 'net',
        'inf', 'trn', 'cls', 'reg', 'tok', 'seq', 'dim', 'res', 'att', 'vit',
        'moe', 'lor', 'rlf', 'dpo', 'sft', 'asr', 'tts', 'ocr', 'ner', 'pos',
        # Dev, Infra, Security, Systems
        'api', 'app', 'dev', 'ops', 'sec', 'git', 'cli', 'sdk', 'gui', 'tui',
        'cpu', 'gpu', 'tpu', 'npu', 'ram', 'ssd', 'sql', 'dbx', 'log', 'mon',
        'web', 'vpn', 'dns', 'tcp', 'udp', 'ssl', 'tls', 'ssh', 'pki', 'key',
        'cmd', 'src', 'bin', 'lib', 'pkg', 'env', 'run', 'job', 'que', 'ipc',
        'pid', 'ptr', 'hex', 'bit', 'byt', 'bus', 'dom', 'css', 'xml', 'rpc',
        'url', 'uri', 'mac', 'ios', 'win', 'lan', 'wan', 'iot', 'iso', 'ide',
        'orm', 'etl', 'csv', 'tsv', 'ack', 'req', 'res', 'nat', 'ftp', 'ipf',
        'p2p', 'dht', 'vms', 'vpc', 'sub', 'asn', 'bgp', 'txt', 'doc', 'pdf',
        'vid', 'pic', 'gif', 'png', 'svg', 'zip', 'tar', 'deb', 'rpm', 'apk'
    ]

    business_finance_terms = [
        'biz', 'pro', 'pay', 'fin', 'ceo', 'cto', 'cfo', 'cmo', 'coo', 'cio',
        'cso', 'cro', 'cpo', 'roi', 'kpi', 'crm', 'erp', 'seo', 'sem', 'ads',
        'tax', 'buy', 'bid', 'ask', 'fee', 'tip', 'val', 'cap', 'ico', 'dex',
        'cex', 'atm', 'gem', 'vip', 'max', 'top', 'one', 'two', 'six', 'ten',
        'all', 'now', 'new', 'win', 'ace', 'pub', 'bar', 'law', 'med', 'bio',
        'eco', 'geo', 'zen', 'gig', 'sol', 'lux', 'nex', 'vox', 'dot', 'tap',
        'aid', 'dao', 'eth', 'btc', 'sol', 'gas', 'nft', 'dex', 'apy', 'apr',
        'usd', 'idr', 'eur', 'gbp', 'jpy', 'sgd', 'cny', 'aud', 'cad', 'id'
    ]

    curated = []
    seen = set()

    def add_list(words):
        for w in words:
            w = w.strip().lower()
            if len(w) == 3 and w.isalpha() and w not in seen:
                seen.add(w)
                curated.append(w)

    # Priority 1: AI / Tech / Cloud
    add_list(ai_tech_terms)
    # Priority 2: Business / Fintech / Brand
    add_list(business_finance_terms)
    # Priority 3: Scrabble words appearing in real-world frequency (true English words)
    add_list([w for w in freq_raw if w in scrabble])
    # Priority 4: Modern internet frequency words
    add_list(freq_raw)
    # Priority 5: High-quality Scrabble dictionary words
    add_list(sorted(list(scrabble)))

    words_1k = curated[:1000]
    print(f"Curated exactly {len(words_1k)} 3-letter words.")
    return words_1k, freq_rank, set(ai_tech_terms), set(business_finance_terms), scrabble

def query_whois(domain, retries=3):
    for attempt in range(retries):
        try:
            s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            s.settimeout(5)
            s.connect(('whois.id', 43))
            s.sendall(f'{domain}\r\n'.encode('utf-8'))
            res = b''
            while True:
                chunk = s.recv(4096)
                if not chunk:
                    break
                res += chunk
            s.close()
            text = res.decode('utf-8', errors='ignore')

            if 'DOMAIN NOT FOUND' in text:
                return {
                    'status': 'AVAILABLE',
                    'registrar': None,
                    'created': None,
                    'expires': None
                }
            elif 'Domain Name:' in text:
                registrar_match = re.search(r'Registrar:\s*(.+)', text)
                created_match = re.search(r'Creation Date:\s*(.+)', text)
                expires_match = re.search(r'Registry Expiry Date:\s*(.+)', text)
                return {
                    'status': 'REGISTERED',
                    'registrar': registrar_match.group(1).strip() if registrar_match else 'Unknown',
                    'created': created_match.group(1).strip() if created_match else 'Unknown',
                    'expires': expires_match.group(1).strip() if expires_match else 'Unknown'
                }
            else:
                # If unexpected response, retry
                time.sleep(0.3 * (attempt + 1))
        except Exception as e:
            time.sleep(0.3 * (attempt + 1))
    return {
        'status': 'ERROR',
        'registrar': None,
        'created': None,
        'expires': None
    }

def categorize_word(w, ai_tech_set, biz_set):
    if w in ['gpt', 'llm', 'rag', 'nlp', 'agi', 'ann', 'rnn', 'cnn', 'knn', 'svm', 'gen', 'bot', 'sim', 'cog', 'neu', 'vec', 'emb', 'fit', 'hub', 'lab', 'mod', 'vis', 'dat', 'eva', 'opt', 'act', 'ctx', 'syn', 'mem', 'net', 'inf', 'trn', 'cls', 'tok', 'seq', 'dim', 'res', 'att', 'vit', 'moe', 'lor', 'rlf', 'dpo', 'sft', 'asr', 'tts', 'ocr', 'ner']:
        return 'AI & Machine Learning'
    elif w in ['api', 'app', 'dev', 'ops', 'sec', 'git', 'cli', 'sdk', 'gui', 'tui', 'cpu', 'gpu', 'tpu', 'npu', 'ram', 'ssd', 'sql', 'dbx', 'log', 'mon', 'web', 'vpn', 'dns', 'tcp', 'udp', 'ssl', 'tls', 'ssh', 'pki', 'key', 'cmd', 'src', 'bin', 'lib', 'pkg', 'env', 'run', 'job', 'que', 'ipc', 'pid', 'ptr', 'hex', 'bit', 'byt', 'bus', 'dom', 'css', 'xml', 'rpc', 'url', 'uri', 'mac', 'ios', 'win', 'lan', 'wan', 'iot', 'iso', 'ide', 'orm', 'etl', 'csv', 'tsv', 'ack', 'req', 'nat', 'ftp', 'txt', 'doc', 'pdf', 'vid', 'pic', 'gif', 'zip']:
        return 'Developer & Cloud Infra'
    elif w in ['biz', 'pro', 'pay', 'fin', 'ceo', 'cto', 'cfo', 'cmo', 'coo', 'cio', 'cso', 'cro', 'cpo', 'roi', 'kpi', 'crm', 'erp', 'seo', 'sem', 'ads', 'tax', 'buy', 'bid', 'ask', 'fee', 'tip', 'val', 'cap', 'ico', 'dex', 'cex', 'atm', 'gem', 'vip', 'max', 'top', 'one', 'two', 'six', 'ten', 'all', 'now', 'new', 'win', 'ace', 'dao', 'eth', 'btc', 'sol', 'gas', 'nft']:
        return 'Fintech & Business'
    elif w in ['run', 'fly', 'get', 'set', 'try', 'see', 'hit', 'cut', 'tap', 'pop', 'let', 'put', 'say', 'eat', 'fit', 'fix', 'go', 'use', 'add', 'ask', 'opt', 'end', 'out']:
        return 'Action Verbs'
    elif w in ['sky', 'sun', 'sea', 'air', 'ice', 'hot', 'red', 'cat', 'dog', 'fox', 'car', 'day', 'eye', 'ray', 'way', 'joy', 'zen', 'art', 'law', 'med', 'bio', 'eco', 'geo', 'box', 'cup', 'tea', 'pie', 'fan', 'gym', 'bed', 'bus', 'pen', 'key', 'map', 'pin', 'tag']:
        return 'Premium Brandable English'
    else:
        return 'Dictionary & Common English'

def calculate_value_score(w, category, freq_rank, is_scrabble):
    score = 0

    # 1. Category Value (0 - 40 pts)
    if category == 'AI & Machine Learning':
        score += 40
    elif category == 'Developer & Cloud Infra':
        score += 35
    elif category == 'Fintech & Business':
        score += 32
    elif category == 'Premium Brandable English':
        score += 28
    elif category == 'Action Verbs':
        score += 25
    else:
        score += 15

    # 2. English Real-World Frequency (0 - 30 pts)
    rank = freq_rank.get(w)
    if rank is not None:
        if rank < 300:
            score += 30
        elif rank < 1000:
            score += 25
        elif rank < 3000:
            score += 20
        elif rank < 7000:
            score += 15
        elif rank < 15000:
            score += 10
        else:
            score += 5
    elif is_scrabble:
        score += 5

    # 3. Phonetic & Brandability Structure (0 - 15 pts)
    vowels = set('aeiouy')
    c1, c2, c3 = w[0], w[1], w[2]
    if c1 not in vowels and c2 in vowels and c3 not in vowels:
        score += 15  # Classic CVC: bot, hub, run, pay, vox, nex, sol
    elif c1 in vowels and c2 not in vowels and c3 in vowels:
        score += 13  # VCV: ace, one, eye, era, opt
    elif c1 in vowels and c2 not in vowels and c3 not in vowels:
        score += 11  # VCC: app, all, act, art, ask
    elif c1 not in vowels and c2 in vowels and c3 in vowels:
        score += 11  # CVV: sea, sky, fly, tea, pie, ray
    else:
        score += 5

    # 4. Universal Punch / CTA Bonus (0 - 15 pts)
    if w in ['gpt', 'llm', 'rag', 'agi', 'api', 'app', 'dev', 'ops', 'bot', 'pay', 'fin', 'pro', 'biz', 'run', 'fly', 'sun', 'sky', 'sea', 'one', 'top', 'max', 'vip', 'ace', 'win', 'hub', 'lab', 'key', 'car', 'box']:
        score += 15
    elif w in ['cat', 'dog', 'fox', 'red', 'ice', 'hot', 'air', 'day', 'eye', 'way', 'joy', 'zen', 'art', 'bio', 'law', 'med', 'eco', 'buy', 'tax', 'tip', 'val', 'cap', 'net', 'web', 'gpu', 'cpu', 'sql', 'git', 'cli', 'sdk']:
        score += 10
    elif w in ['see', 'try', 'get', 'set', 'tap', 'dot', 'sol', 'lux', 'nex', 'vox', 'gen', 'sim', 'mod', 'vis', 'dat', 'mem']:
        score += 8

    return min(score, 100)

def assign_tier(score):
    if score >= 85:
        return 'Tier 1: Ultra-Premium'
    elif score >= 70:
        return 'Tier 2: High Value'
    elif score >= 55:
        return 'Tier 3: Good Brandable'
    elif score >= 40:
        return 'Tier 4: Standard'
    else:
        return 'Tier 5: Niche / Scrabble'

def main():
    words_1k, freq_rank, ai_tech_set, biz_set, scrabble_set = get_wordlists()
    
    print(f"Beginning WHOIS verification for {len(words_1k)} domains against whois.id...")
    results = []
    t_start = time.time()
    
    # Run concurrent WHOIS queries with 8 workers
    with ThreadPoolExecutor(max_workers=8) as executor:
        future_to_word = {
            executor.submit(query_whois, f"{w}.ai.id"): w for w in words_1k
        }
        completed_count = 0
        for future in as_completed(future_to_word):
            w = future_to_word[future]
            try:
                res = future.result()
            except Exception as e:
                res = {'status': 'ERROR', 'registrar': None, 'created': None, 'expires': None}
            
            domain = f"{w}.ai.id"
            category = categorize_word(w, ai_tech_set, biz_set)
            is_scrabble = w in scrabble_set
            score = calculate_value_score(w, category, freq_rank, is_scrabble)
            tier = assign_tier(score)
            
            record = {
                'word': w,
                'domain': domain,
                'status': res['status'],
                'score': score,
                'tier': tier,
                'category': category,
                'freq_rank': freq_rank.get(w, 99999),
                'registrar': res['registrar'] or '',
                'created': res['created'] or '',
                'expires': res['expires'] or ''
            }
            results.append(record)
            completed_count += 1
            if completed_count % 100 == 0:
                print(f"Progress: {completed_count}/{len(words_1k)} checked ({time.time() - t_start:.1f}s)...")

    # Sort results by score descending, then by word ascending
    results.sort(key=lambda x: (-x['score'], x['freq_rank'], x['word']))

    # Statistics
    total = len(results)
    available = [r for r in results if r['status'] == 'AVAILABLE']
    registered = [r for r in results if r['status'] == 'REGISTERED']
    errors = [r for r in results if r['status'] not in ('AVAILABLE', 'REGISTERED')]

    print("\n--- RESULTS SUMMARY ---")
    print(f"Total Domains Checked: {total}")
    print(f"Available: {len(available)} ({len(available)/total*100:.1f}%)")
    print(f"Registered: {len(registered)} ({len(registered)/total*100:.1f}%)")
    print(f"Errors/Unknown: {len(errors)}")

    # Write CSV
    csv_file = '/home/agent/orca/learnings/ai_id_1000_domains.csv'
    fieldnames = ['rank', 'word', 'domain', 'status', 'score', 'tier', 'category', 'registrar', 'created', 'expires']
    with open(csv_file, 'w', newline='', encoding='utf-8') as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        for idx, r in enumerate(results, start=1):
            row = {
                'rank': idx,
                'word': r['word'],
                'domain': r['domain'],
                'status': r['status'],
                'score': r['score'],
                'tier': r['tier'],
                'category': r['category'],
                'registrar': r['registrar'],
                'created': r['created'],
                'expires': r['expires']
            }
            writer.writerow(row)
    print(f"Saved CSV to {csv_file}")

    # Write JSON
    json_file = '/home/agent/orca/learnings/ai_id_1000_domains.json'
    with open(json_file, 'w', encoding='utf-8') as f:
        json.dump({
            'meta': {
                'generated_at': time.strftime('%Y-%m-%d %H:%M:%S UTC', time.gmtime()),
                'total_checked': total,
                'available_count': len(available),
                'registered_count': len(registered),
                'availability_pct': round(len(available)/total*100, 2)
            },
            'results': results
        }, f, indent=2)
    print(f"Saved JSON to {json_file}")

if __name__ == '__main__':
    main()
