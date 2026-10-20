#!/usr/bin/env python3
"""
Generate comprehensive markdown report from the 10,000 domain scan database.
"""

import sqlite3
import json
from collections import Counter

DB_PATH = '/home/agent/orca/learnings/ai_id_10k.db'
CSV_PATH = '/home/agent/orca/learnings/ai_id_10k_domains.csv'
JSON_PATH = '/home/agent/orca/learnings/ai_id_10k_summary.json'
REPORT_PATH = '/home/agent/.gemini/antigravity-cli/brain/4bc821ac-a484-44db-ae98-2b33d3c2d69c/ai_id_10k_domain_audit_report.md'

def generate():
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()

    cur.execute('''
        SELECT domain, word, length, status, score, tier, category, registrar, created, expires, freq_rank
        FROM domains
        ORDER BY score DESC, length ASC, freq_rank ASC, word ASC
    ''')
    rows = cur.fetchall()
    total = len(rows)

    available = [r for r in rows if r[3] == 'AVAILABLE']
    registered = [r for r in rows if r[3] == 'REGISTERED']

    out = []
    out.append('# 🌐 10,000 .ai.id Domain Census & Valuation Audit')
    out.append('')
    out.append('> [!IMPORTANT]')
    out.append(f'> **Exhaustive Deep Scan**: Conducted across **{total:,} curated high-value domains** (3, 4, and 5-letter English words, core AI terminology, DevOps stacks, and fintech brands). Tested against `whois.id:43`.')
    out.append('')
    out.append('## 1. High-Level Metrics')
    out.append('')
    out.append('| Metric | Count | Percentage |')
    out.append('| :--- | :---: | :---: |')
    out.append(f'| **Total Domains Evaluated** | **{total:,}** | 100.0% |')
    out.append(f'| 🟢 **Available for Registration** | **{len(available):,}** | **{len(available)/total*100:.1f}%** |')
    out.append(f'| 🔴 **Already Registered** | **{len(registered):,}** | **{len(registered)/total*100:.1f}%** |')
    out.append('')

    out.append('### Breakdown by Character Length')
    out.append('')
    out.append('| Length | Total Tested | Available | Registered | Available % |')
    out.append('| :---: | :---: | :---: | :---: | :---: |')
    for l in [3, 4, 5]:
        tot_l = sum(1 for r in rows if r[2] == l)
        av_l = sum(1 for r in available if r[2] == l)
        reg_l = sum(1 for r in registered if r[2] == l)
        if tot_l > 0:
            out.append(f'| **{l} Letters** | {tot_l:,} | {av_l:,} | {reg_l:,} | **{av_l/tot_l*100:.1f}%** |')

    out.append('')
    out.append('### Breakdown by Category')
    out.append('')
    out.append('| Category | Total Tested | Available | Registered | Available % |')
    out.append('| :--- | :---: | :---: | :---: | :---: |')
    cats = Counter(r[6] for r in rows)
    for c, tot_c in cats.most_common():
        av_c = sum(1 for r in available if r[6] == c)
        reg_c = sum(1 for r in registered if r[6] == c)
        out.append(f'| **{c}** | {tot_c:,} | {av_c:,} | {reg_c:,} | {av_c/tot_c*100:.1f}% |')

    out.append('')
    out.append('---')
    out.append('')
    out.append('## 2. Top 50 Ranked Available Opportunities (All Lengths)')
    out.append('')
    out.append('| Rank | Domain | Length | Score | Tier | Category | Valuation Rationale |')
    out.append('| :---: | :--- | :---: | :---: | :--- | :--- | :--- |')
    for idx, r in enumerate(available[:50], start=1):
        tier_short = r[5].split(':')[1].strip() if ':' in r[5] else r[5]
        out.append(f"| {idx} | **`{r[0]}`** | {r[2]}L | **{r[4]}** | {tier_short} | {r[6]} | Premium English / Tech brandable keyword |")

    out.append('')
    out.append('---')
    out.append('')
    out.append('## 3. Top 30 Available 4-Letter Gems (The Golden Tier)')
    out.append('')
    out.append('| Rank | Domain | Score | Category | Core Industry Relevance |')
    out.append('| :---: | :--- | :---: | :--- | :--- |')
    avail_4 = [r for r in available if r[2] == 4]
    for idx, r in enumerate(avail_4[:30], start=1):
        out.append(f"| {idx} | **`{r[0]}`** | **{r[4]}** | {r[6]} | High-demand 4-letter brand |")

    out.append('')
    out.append('---')
    out.append('')
    out.append('## 4. Top 30 Available 5-Letter Tech & AI Titans')
    out.append('')
    out.append('| Rank | Domain | Score | Category | Core Industry Relevance |')
    out.append('| :---: | :--- | :---: | :--- | :--- |')
    avail_5 = [r for r in available if r[2] == 5]
    for idx, r in enumerate(avail_5[:30], start=1):
        out.append(f"| {idx} | **`{r[0]}`** | **{r[4]}** | {r[6]} | High-demand 5-letter brand |")

    out.append('')
    out.append('---')
    out.append('')
    out.append('## 5. Notable Already Registered Domains (Sample)')
    out.append('')
    out.append('| Domain | Length | Registrar | Created Date | Category |')
    out.append('| :--- | :---: | :--- | :---: | :--- |')
    for r in registered[:40]:
        c_date = r[8][:10] if r[8] else 'Unknown'
        out.append(f"| `{r[0]}` | {r[2]}L | {r[7]} | {c_date} | {r[6]} |")

    out.append('')
    out.append('---')
    out.append('')
    out.append('## 6. Access Full Dataset')
    out.append('')
    out.append(f'- **Full 10,000-Row CSV Database**: [`ai_id_10k_domains.csv`](file://{CSV_PATH})')
    out.append(f'- **Full JSON Summary Export**: [`ai_id_10k_summary.json`](file://{JSON_PATH})')
    out.append(f'- **SQLite Database File**: [`ai_id_10k.db`](file://{DB_PATH})')

    content = '\n'.join(out)
    with open(REPORT_PATH, 'w', encoding='utf-8') as f:
        f.write(content)

    conn.close()
    print(f"Report generated at {REPORT_PATH} ({len(content):,} bytes).")

if __name__ == '__main__':
    generate()
