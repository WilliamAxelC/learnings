# Colo VPS Venture Blueprint: Low-Cost CPU Infrastructure Playbook

---

## Executive Overview & Strategic Thesis

This document outlines the end-to-end commercial, financial, and technical strategy for launching a boutique Virtual Private Server (VPS) and compute infrastructure provider using refurbished/off-lease enterprise CPU servers deployed in colocation data centers.

The market for public cloud compute (AWS, Google Cloud, Azure) carries severe markup: an equivalent 4 vCPU / 8 GB RAM instance costs $30–$48/month on hyperscalers, with predatory egress bandwidth charges ($0.05–$0.09/GB). Meanwhile, rock-bottom "budget VPS" providers ($2–$3/month) suffer from catastrophic support churn, severe noisy-neighbor CPU oversubscription, and frequent insolvencies caused by power inefficiency.

Our operational wedge is a **density-optimized, single-socket AMD EPYC bare-metal hypervisor fleet** located in Tier-2/Tier-3 metropolitan colocation facilities, targeting **developers, self-hosters, and local SMB/agency dev teams** at a sustainable price point of **$6–$24/month** with zero egress fees and dedicated ECC memory.

---

## Stage 1: Strategy & Diagnosis (Rumelt's Kernel)

```
+---------------------------------------------------------------------------------------+
|                                    1. DIAGNOSIS                                       |
|  • Power & Rack Density vs. Server Watts: Colo rack space is cheap; POWER is deadly.  |
|  • IPv4 Scarcity Tax: $0.45/mo per IP wipes out 20% of low-end ($2-$3) margins.      |
|  • The Abuse/DDoS Trap: Cheap boxes attract botnets, spam, and unmitigated transit.   |
|  • Support Ticket Bleed: High churn & low ARPU kill single-operator economics.        |
+---------------------------------------------------------------------------------------+
                                           │
                                           ▼
+---------------------------------------------------------------------------------------+
|                                  2. GUIDING POLICY                                    |
|  • Zero Ancient Silicon: Reject cheap Dual-Xeon heaters; mandate 7nm AMD EPYC Rome.  |
|  • Avoid the $2 Race-to-the-Bottom: Anchor entry price at $6/mo (30-40% gross margin).|
|  • Fraud-Gated Automation: Strict MaxMind scoring + port 25 blocking by default.     |
|  • 10U Expansion Gates: Deploy in quarter-rack increments; expand only at 75% load.  |
+---------------------------------------------------------------------------------------+
                                           │
                                           ▼
+---------------------------------------------------------------------------------------+
|                                 3. COHERENT ACTIONS                                   |
|  • Action 1: Procure 1x Supermicro 1U EPYC 7542 (32C/64T, 256GB ECC, 4x NVMe).      |
|  • Action 2: Secure 10U Quarter-Rack colo with 1kVA power + 1Gbps unmetered + /24 IP. |
|  • Action 3: Deploy automated stack: Proxmox/Virtualizor + Blesta + BGP Flowspec.     |
|  • Action 4: Launch beachhead presale across dev communities (cap at 60 instances).  |
+---------------------------------------------------------------------------------------+
```

### 1. Diagnosis: The Real Bottlenecks

The budget VPS graveyard is filled with hosts that died within 12–18 months. The structural reasons are:

1. **The Power Trap (Watts per Core):**
   Amateurs buy cheap off-lease enterprise servers (e.g., Dell R620/R720 or dual Intel Xeon E5-2670 v2/v3) on eBay for $150–$250. These dual-socket machines draw **320W–450W** under load. In colocation, rack space is cheap ($300–$500/month for a half rack), but **power costs $20–$35 per Amp at 120V ($150–$250 per kW)**. Running a 400W server costs **$60–$100/month in power alone**. Saving $1,000 upfront on obsolete hardware costs $2,400+ per server annually in electricity.
2. **The IPv4 Overhead:**
   An ARIN/RIPE IPv4 /24 subnet (256 addresses, 253 usable) costs ~$9,000–$11,000 to purchase or $0.40–$0.55/IP/month to lease ($102–$140/month). When a provider sells a VPS for $2.50/month, the IPv4 lease consumes 20% of gross revenue before factoring in hardware, bandwidth, or colocation.
3. **The Abuse and Blacklist Sinkhole:**
   Low prices attract fraudsters, bulletproof hosting seekers, and botnet operators. A single outbound spam wave gets the provider’s entire /24 subnet blacklisted on Spamhaus SBL/CSS, ruining IP reputation for legitimate clients. Unmitigated inbound UDP amplification floods saturate upstreams, forcing data centers to null-route the rack.
4. **ARPU vs. Support Cost:**
   A customer paying $3/month who submits three support tickets per month about basic Linux administration generates an immediate net loss.

### 2. Guiding Policy

We rule out the entire "race to the bottom" commodity hosting segment. We do not sell $1.50/month VPSs.

1. **Silicon Efficiency Policy:** Only single-socket or high-efficiency dual-socket 7nm/10nm architecture (AMD EPYC 7002 Rome or EPYC 7003 Milan). Target system power envelope: **≤225W per 64 logical threads**.
2. **Pricing Floor:** Entry-level tier is anchored at **$6.00/month (1 vCPU / 2GB RAM / 30GB NVMe)**. This absorbs the IPv4 lease cost ($0.45), payment processing ($0.47), and power allocation ($1.20) while retaining healthy contribution margin.
3. **Zero-Trust Onboarding:**
   - Outbound SMTP (ports 25, 465, 587) blocked at the host firewall by default. KYC identity verification required to unlock email ports.
   - MaxMind minFraud score thresholding on all new signups; automatic decline for Tor exit nodes, high-risk proxies, and prepaid anonymous cards.
4. **Lean Footprint Rollout:** Begin with a **10U (Quarter-Rack) / 1kVA** contract. Do not contract a full 42U cabinet until node occupancy crosses 80% across 4 hypervisors.

### 3. Coherent Actions

| Action | Resource / Budget | Timeline | Completion Criteria |
| :--- | :--- | :--- | :--- |
| **Action 1: Hardware Procurement** | $1,770 CapEx | Week 1–2 | 1U Supermicro server assembled, memtest86 48h burn-in, NVMe fio stress test passed. |
| **Action 2: Colocation & Transit Turn-Up** | $565 setup + $450/mo | Week 2–3 | 10U rack space active, 1Gbps uplink passing BGP/VLAN traffic, /24 subnet routed. |
| **Action 3: Automation Stack Setup** | $25/mo licenses | Week 3–4 | Virtualizor/KVM + Blesta billing integrated with Stripe, automated VM provisioning < 60s. |
| **Action 4: Beachhead Commercial Launch** | $0 marketing (Organic) | Week 4–6 | 60 pilot instances sold to local developer and homelab community; breakeven achieved. |

---

### 4. Create-Destroy Stress Test

```mermaid
flowchart TD
    A["Attack: Upstream Power Tariff Spike (+30%)"] --> B["Defense: EPYC 7542 draws only 220W for 120 VMs ($0.08/VM extra impact)"]
    C["Attack: Subnet Blacklisting via Outbound Spam"] --> D["Defense: Hardware egress drop on Port 25; KYC required for mail unlocks"]
    E["Attack: 100 Gbps DDoS saturation of 1Gbps port"] --> F["Defense: BGP Blackhole / Upstream Flowspec auto-nulls target IP, protecting rack"]
    G["Attack: Slow customer acquisition (Worst-Case)"] --> H["Defense: Hardware resale floor recovers 81% of CapEx ($1,440 of $1,770)"]
```

#### What Destroys the Model & How It Is Defended:
- **Destruction Vector 1: Rapid Hardware Failure (NVMe / RAM):**
  - *Risk:* Consumer SSDs (e.g., Samsung 980 Pro) have high write amplification in VM environments and fail within 6 months.
  - *Defense:* Strictly deploy Enterprise U.2/U.3 NVMe drives (Samsung PM9A3 or Micron 7450 PRO) rated for 1–3 DWPD with Power Loss Protection (PLP) in hardware RAID 10.
- **Destruction Vector 2: Subnet Reputational Contamination:**
  - *Risk:* Spamhaus blacklists the entire /24 subnet because one tenant rented a VM to send phishing emails.
  - *Defense:* Port 25/465 is permanently filtered upstream at the router. Automated rDNS enforcement. Any abuse complaint results in immediate automated VM suspension within 15 minutes.
- **Destruction Vector 3: Liquidation Floor (Downside Guarantee):**
  - If zero paying customers sign up after 6 months, the enterprise components hold strong secondary market liquidity:
    - 4x 1.92TB Enterprise NVMe: $520 resale
    - 256GB DDR4 ECC Reg RAM: $220 resale
    - AMD EPYC 7542 CPU: $300 resale
    - Supermicro Motherboard & 1U Chassis: $400 resale
    - **Total Hardware Liquidation Recovery: ~$1,440 (81.3% of the initial $1,770 server CapEx).**

---

## Stage 2: Single-Source-of-Truth Financial Model & Unit Economics

### 1. Hardware Bill of Materials (Per Hypervisor Node)

All financial projections reconcile back to this exact hardware specification:

| Item | Component Specification | Source / Condition | Unit Cost (USD) |
| :--- | :--- | :--- | :--- |
| **CPU** | AMD EPYC 7542 (32 Cores, 64 Threads, 2.9–3.4 GHz, 128MB L3) | Refurbished / Enterprise | $320 |
| **Motherboard** | Supermicro H11SSL-i / H12SSL-i (PCIe 4.0, IPMI 2.0 / KVM-over-IP) | Refurbished / New Old Stock | $350 |
| **RAM** | 256 GB (8x 32GB) DDR4-3200MHz ECC Registered RDIMM | Refurbished Grade A | $280 |
| **Primary Storage** | 4x 1.92TB Enterprise NVMe U.2 (Samsung PM9A3 / Micron 7450 Pro) | Brand New / Clean Smart | $660 ($165 ea) |
| **Chassis & Power** | Supermicro 1U Short-Depth w/ Dual 500W–800W 80+ Titanium PSUs | Refurbished | $175 |
| **Networking** | Dual-Port 10GbE SFP+ (Intel X520-DA2 or Mellanox ConnectX-3) | Refurbished | $45 |
| **Accessories** | NVMe OCuLink cables, Slim SAS, 1U passive heatsink, rack rails | New | $60 |
| **Per-Node Server CapEx** | | | **$1,890** |
| **Shared Network Switch** | 1x MikroTik CRS309-1G-8S+IN (8x 10G SFP+, managed L3 switch) | New | $260 |
| **Initial Setup / Logistics** | Packaging, remote hands rack & stack, cable management | Direct | $150 |
| **Total Initial Deployment CapEx (1 Node + Switch):** | | | **$2,300** |

---

### 2. Operational Expenses (OpEx Breakdown)

| Cost Category | Scope & Commitment | Monthly Cost (USD) |
| :--- | :--- | :--- |
| **Colocation Space & Power** | Quarter-Rack (10U) with 1kVA (8A @ 120V or 4.8A @ 208V), redundant A+B feeds | $350.00 |
| **Network Transit Uplink** | 1 Gbps unmetered blended bandwidth (Cogent + Hurricane Electric / NTT) | $100.00 |
| **IPv4 Address Allocation** | 1x /24 Subnet lease (256 addresses total, 253 usable client IPs) @ $0.45/IP | $115.00 |
| **Hypervisor Licensing** | Virtualizor KVM License (Unlimited VPS per server) | $10.00 |
| **Billing & Client Automation** | Blesta Unbranded Owned/Leased license | $14.95 |
| **Offsite Backups & Monitoring** | Wasabi / Backblaze B2 (2TB encrypted offsite snapshots) + UptimeRobot | $15.00 |
| **Total Baseline Monthly OpEx (Node 1 Active):** | | **$604.95** |

> [!NOTE]
> When adding Node 2 into the existing 10U space, the rack, bandwidth, and IPv4 leases are **already paid for**. Node 2 only incurs an incremental **$10.00/mo Virtualizor license** and approximately **$30.00/mo incremental power**, dramatically multiplying operating margins.

---

### 3. Hypervisor Capacity & VPS Product Tiering

**Usable Physical Pool per Node:**
- Total Cores / Threads: 32 Cores / 64 Threads. *Reserved for Host OS: 4 Threads.* **Usable: 60 Threads.**
- Total RAM: 256 GB ECC DDR4. *Reserved for Host OS/ZFS cache: 16 GB.* **Usable: 240 GB.**
- Total Storage: 4x 1.92TB NVMe in RAID 10 = ~3.84 TB raw. *Reserved for Host & Overprovisioning: 240 GB.* **Usable: 3,600 GB Enterprise NVMe.**

#### Product Tiers & Slicing Matrix

| Tier Name | vCPU | Dedicated RAM | NVMe Storage | Bandwidth | Monthly Price | Max Density / Node |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Micro Dev** | 1 vCPU | 2 GB ECC | 30 GB NVMe | 2 TB @ 1 Gbps | **$6.00** | 120 Instances |
| **Standard Core** | 2 vCPU | 4 GB ECC | 60 GB NVMe | 4 TB @ 1 Gbps | **$12.00** | 60 Instances |
| **Pro Compute** | 4 vCPU | 8 GB ECC | 120 GB NVMe | 8 TB @ 1 Gbps | **$24.00** | 30 Instances |
| **Power Node** | 8 vCPU | 16 GB ECC | 240 GB NVMe | 16 TB @ 1 Gbps | **$48.00** | 15 Instances |

*Density Benchmark:* Slicing by RAM (strict 0% overcommitment on RAM to guarantee zero OOM swapping):
- 240 GB usable / 2 GB per Micro VM = **120 instances maximum**.
- 120 instances across 60 vCPU threads = **2.0 : 1 vCPU oversubscription ratio** (standard industry best practice; guarantees responsive CPU performance with zero steal time).
- 120 instances × 30 GB NVMe = 3,600 GB NVMe (100% storage utilization).
- 120 instances × $6.00/month = **$720.00 Gross Monthly Revenue per Node at 100% occupancy**.
- Blended Tier Mix (40% Micro @ $6, 40% Standard @ $12, 20% Pro @ $24) produces an average revenue per user (ARPU) of **$12.00** across 40 VM instances = **$720.00 Gross Monthly Revenue**.

---

### 4. Financial Sensitivity Analysis (Bear / Base / Bull)

```
Monthly Net Profit Comparison:
Bear Case (1 Node @ 50% Occupancy):    -$245 / mo   (Cash preservation mode)
Base Case (2 Nodes @ 80% Occupancy):   +$505 / mo   (Payback in 8.3 months)
Bull Case (4 Nodes @ 85% Occupancy):   +$1,500 / mo (High-yield cash generator)
```

| Metric | Bear Case (1 Node, Slow Growth) | Base Case (2 Nodes, Target Model) | Bull Case (4 Nodes, Full 10U Rack) |
| :--- | :--- | :--- | :--- |
| **Active Nodes** | 1 Node | 2 Nodes | 4 Nodes |
| **Total Hardware CapEx** | $2,300 | $4,190 ($2,300 + $1,890) | $7,970 ($2,300 + 3×$1,890) |
| **Capacity Utilization** | 50% Occupancy | 80% Occupancy | 85% Occupancy |
| **Active Client VMs** | 30 VMs (Blended mix) | 64 VMs (Blended mix) | 136 VMs (Blended mix) |
| **Gross Monthly Revenue** | **$360.00** | **$1,152.00** | **$2,448.00** |
| **Colocation & Power OpEx** | $350.00 | $380.00 (Power add-on) | $470.00 (Power upgrade) |
| **Bandwidth & Uplink** | $100.00 | $100.00 | $150.00 (Burst upgrade) |
| **IPv4 Leases** | $115.00 (1x /24) | $115.00 (1x /24) | $230.00 (2x /24 subnets) |
| **Software & Incidentals** | $39.95 | $51.95 | $98.00 |
| **Total Monthly OpEx** | **$604.95** | **$646.95** | **$948.00** |
| **Monthly Net Profit (Loss)** | **-$244.95** | **+$505.05** | **+$1,500.00** |
| **Annualized Net Operating Cash Flow**| -$2,939.40 | **+$6,060.60** | **+$18,000.00** |
| **CapEx Payback Period** | N/A (Breakeven at 85% on 1 Node)| **8.3 Months** | **5.3 Months** |

---

## Stage 3: Investor Pitch Deck & 1-Page Investment Memo

### 1-Page Investment Memo

```
================================================================================
INVESTMENT MEMORANDUM: BARE-METAL HYPERVISOR VENTURE
Target Seed Allocation: $15,000 USD | Stage: Pre-Seed Infrastructure
Contact: founders@rackinfra.local | Target Facility: Equinix / Coresite Metro
================================================================================

1. THE THESIS
Hyperscaler cloud margins (AWS EC2, Google Cloud) exceed 60% due to enterprise 
convenience premiums and extortionate egress bandwidth fees. Concurrently, 
budget VPS providers run obsolete 10-year-old dual Intel Xeon servers that 
draw 400W and generate catastrophic power bills, leading to frequent host 
failures. By deploying high-density, power-efficient single-socket AMD EPYC 
processors (7nm) into tier-2 colocation facilities, we achieve an unfair power-
to-core advantage: delivering enterprise-grade NVMe compute at 70% below AWS 
pricing while securing >60% gross margins at steady-state.

2. THE ENGINE & UNIT ECONOMICS
- CapEx per 64-Thread Hypervisor Node: $1,890 (Fully amortized over 24 months).
- Monthly Rack & Power Footprint (10U / 1kW): $350/mo.
- Hardware Yield: 1 Hypervisor supports 40–120 isolated VMs generating $720/mo.
- 2-Node Cluster Base Case: $1,152/mo revenue on $647/mo OpEx = +$505/mo net cash.
- Full 4-Node Rack Bull Case: $2,448/mo revenue on $948/mo OpEx = +$1,500/mo net cash.

3. USE OF FUNDS ($15,000 ALLOCATION)
- Hardware Fleet CapEx (4x EPYC Nodes + 10G Switch + PDU):  $8,200
- Colocation Prepayments & IPv4 Allocation (/23 Reservation): $3,800
- Working Capital & 6-Month OpEx Runway:                     $3,000
================================================================================
```

---

### 10-Slide Pitch Flow

#### Slide 1: The Wedge (Title)
- **Title:** Enterprise-Class VPS Infrastructure on Lean Unit Economics.
- **Sub-headline:** Eliminating hyperscaler egress markup with high-density AMD EPYC hypervisors deployed in Tier-2 metro colocation.

#### Slide 2: The Problem (Market Crux)
- Developers and engineering teams are trapped between two broken options:
  1. **Hyperscalers (AWS / GCP / Azure):** $30–$48/mo for 4 vCPU / 8 GB RAM, plus punishing $0.09/GB egress bandwidth bills.
  2. **Budget VPS Hosts:** $2–$3/mo bottom-feeders running 12-year-old power-hungry Xeon servers that crash under noisy-neighbor loads, lack enterprise NVMe storage, and frequently go bankrupt.

#### Slide 3: The Solution
- **The Modern Density Sweet-Spot:**
  - Single-socket 7nm AMD EPYC Rome silicon (225W total system envelope).
  - Pure PCIe 4.0 Enterprise NVMe in RAID 10 with Power Loss Protection (PLP).
  - Unmetered 1Gbps / 10Gbps transit with zero egress surcharges.
  - Transparent developer pricing: $6.00 to $24.00/month.

#### Slide 4: Technical Architecture & Topology

```mermaid
flowchart TD
    subgraph DataCenter["Tier-2 Colocation Facility (10U Quarter-Rack)"]
        FeedA["A+B Redundant Power (1kVA / 120V or 208V)"] --> PDU["Metred APC Zero-U PDU"]
        Transit["Dual Upstream Transit (Cogent + HE / NTT)"] --> Switch["MikroTik CRS309 10G SFP+ Switch"]
        
        Switch -->|10G Direct SFP+| Node1["Node 01: AMD EPYC 7542 (32C/64T, 256GB ECC, 4x NVMe)"]
        Switch -->|10G Direct SFP+| Node2["Node 02: AMD EPYC 7542 (32C/64T, 256GB ECC, 4x NVMe)"]
        Switch -->|10G Direct SFP+| Storage["Node 03 / Storage Backup Target"]
        
        Node1 --> KVM1["Proxmox / Virtualizor KVM Engine"]
        Node2 --> KVM2["Proxmox / Virtualizor KVM Engine"]
    end
    
    KVM1 --> Clients["60–120 Isolated Client Virtual Machines"]
    KVM2 --> Clients2["60–120 Isolated Client Virtual Machines"]
```

#### Slide 5: Market Size & Target Beachhead
- **Total Addressable Market (TAM):** $68B Global Cloud VPS and Dedicated Hosting Market.
- **Serviceable Available Market (SAM):** $4.2B Independent Developer, Freelancer, and SMB Web Infrastructure market.
- **Serviceable Obtainable Market (SOM):** $180,000 ARR across 250 local software agencies, CI/CD pipeline builders, and container homelab operators seeking dedicated RAM and zero-metered bandwidth.

#### Slide 6: Unit Economics & Payback Profile
- **Node CapEx:** $1,890.
- **Node OpEx Contribution:** ~$30/mo incremental power once rack is committed.
- **Monthly Revenue Yield per Node:** $720.00.
- **Contribution Margin:** **>78%** per incremental node.
- **Hardware Payback Window:** 8.3 months in Base Case; 5.3 months at 4-node saturation.

#### Slide 7: Traction & Pilot Validation
- Hardware prototype tested under 72-hour continuous Linpack and fio benchmark runs.
- 0% thermal throttling under 220W package power; average drive write latency < 45µs.
- 15 beta developers pre-registered for initial Node 1 launch batch.

#### Slide 8: Defensibility & Structural Moat
1. **Power-Efficiency Asymmetry:** Our 7nm single-socket architecture yields 0.28W per logical thread compared to legacy dual-Xeon competitors running 1.1W per thread (a **4x power cost advantage**).
2. **Owned Hardware vs. Reseller Layer:** Zero leasing overhead or cloud middleman markup.
3. **Automated Fraud & Egress Filtering:** Protects IP subnet reputation, eliminating the single greatest cause of host shutdowns.

#### Slide 9: Competitive Grid

| Feature / Metric | Hyperscalers (AWS / GCP) | Budget Hosts (LowEnd) | Our Infrastructure Model |
| :--- | :--- | :--- | :--- |
| **4 vCPU / 8 GB Monthly Cost** | $32.00 – $48.00 | $4.00 – $7.00 | **$24.00** |
| **Egress Bandwidth Fee** | $0.05 – $0.09 per GB | "Unmetered" (Throttled) | **Zero Egress Fees (Dedicated Port)** |
| **Storage Architecture** | Network EBS (High latency) | Cheap Consumer SATA SSD | **Enterprise PCIe 4.0 NVMe (RAID 10)** |
| **CPU Performance Stability** | Throttled Burstable Credits | 8:1 Overcommit (Severe Steal) | **Strict 2:1 Limit (Zero Steal)** |
| **Outbound Email Abuse Filter** | Strict SES lockdown | Unfiltered (Blacklist risk) | **Default Closed Port 25 + KYC Unlock** |

#### Slide 10: The Ask & Use of Funds
- **Seed Investment Ask:** **$15,000 USD**.
- **Runway:** 12 months to full self-sustaining cash flow.
- **Target Milestones:**
  - Month 2: Turn up 10U Rack + Deploy Nodes 1 & 2 (120 active VMs).
  - Month 6: Reach cash-flow breakeven ($1,150/mo revenue).
  - Month 10: Deploy Nodes 3 & 4 (Fill 10U rack; achieve $1,500/mo net cash flow).

---

## Stage 4: Commercial B2B Proposal & Scope of Work (SOW)

For marketing private VM pools, agency staging environments, or isolated slices directly to regional software development firms.

### B2B Proposal Template

```
================================================================================
COMMERCIAL SCOPE OF WORK & SERVICE PROPOSAL
Client: Apex Digital Agency / Engineering Team
Provider: High-Performance Managed Infrastructure
Date: October 2026 | Document ID: SOW-2026-VPS-01
================================================================================

1. EXECUTIVE SUMMARY
Apex Digital requires dedicated, reliable staging and production virtual server 
capacity for its web applications and client microservices. Hyperscaler public 
clouds impose unpredictable egress fees and volatile CPU stealing during peak 
load. We propose providing a Dedicated Virtual Private Server Pool hosted on 
enterprise AMD EPYC NVMe hardware, featuring guaranteed memory allocation, 
unmetered bandwidth, and a 99.95% uptime SLA at a flat monthly rate.

2. SCOPE OF DELIVERABLES & TECHNICAL SPECIFICATION
+-------------------------------------------------------------------------------+
| Deliverable      | Specification & Architecture         | Acceptance Criteria |
+-------------------------------------------------------------------------------+
| S-01: Agency Dev | 4 Dedicated Virtual Machines         | Pass 24-hour fio    |
| Cluster          | - 4 vCPU (AMD EPYC 3.2GHz+) each     | storage benchmark   |
|                  | - 8 GB DDR4 ECC RAM per node         | (>100k IOPS);       |
|                  | - 120 GB Enterprise NVMe (RAID 10)   | 0% packet loss      |
|                  | - 1x Static IPv4 + /64 IPv6          | across 10k pings.   |
|                  | - Unmetered 1 Gbps Bandwidth         |                     |
+-------------------------------------------------------------------------------+
| S-02: Control    | Self-service web console for reboot, | Automated OS reload |
| Portal           | OS reinstall (Debian/Ubuntu/Alma),   | functions completed |
|                  | VNC console, and snapshot management.| in under 90 seconds.|
+-------------------------------------------------------------------------------+
| S-03: Security & | Hardware firewall profile, DDoS      | Verification of BGP |
| Network Routing  | mitigation up to 40 Gbps, automated  | nightly offsite     |
|                  | nightly offsite image snapshot.      | image snapshot.     |
+-------------------------------------------------------------------------------+

3. PRICING & TIERED OPTIONS
Option A: Standard Agency Pool (Recommended)
- 4x Pro Compute VMs (4 vCPU / 8 GB RAM / 120 GB NVMe each)
- Total Allocation: 16 vCPU, 32 GB RAM, 480 GB NVMe
- Monthly Subscription: $90.00 / month (Flat, zero egress billing)
- Setup & Provisioning Fee: $0.00 (Waived with 6-month commitment)

Option B: Dedicated Node Slice (High Density)
- 1/2 Physical Node Slice (32 vCPU, 120 GB RAM, 1.8 TB NVMe)
- Monthly Subscription: $280.00 / month

4. SERVICE LEVEL AGREEMENT (SLA) & GUARANTEES
- Network & Power Availability: 99.95% monthly uptime.
- Outage Credit Schedule:
  * 99.0% - 99.94%: 10% credit of monthly billing.
  * 95.0% - 98.99%: 25% credit of monthly billing.
  * < 95.0%: 50% credit of monthly billing.
- Hardware Replacement Guarantee: Failed NVMe drive or PSU replaced < 2 hours.

5. TERMS & ACCEPTANCE
- Invoicing: Net-15 monthly recurring via Credit Card / ACH.
- Termination: 30-day written notice; no lock-in contracts after initial term.

Approved and Accepted By:

_________________________________________      _________________________________
Authorized Signatory (Apex Digital)            Date
================================================================================
```

---

## Stage 5: Rapid Outbound Delivery

### Direct Community Pitch (LowEndTalk / Reddit / Discord / Slack)
*Rule: Strictly under 15 lines. Un-slopped. Technical specifications and direct numbers.*

```
Hey folks – just stood up our first 10U quarter-rack in our metro datacenter and looking for 15 initial dev testers.

Hardware specs:
• Single-socket AMD EPYC 7542 (32 Cores / 64 Threads @ 3.4GHz)
• 256GB ECC DDR4 memory (strict 0% RAM overcommit)
• 4x 1.92TB Enterprise NVMe (Samsung PM9A3) in RAID 10 w/ Power Loss Protection
• Redundant 10Gbps SFP+ uplinks on MikroTik L3 switching

The deal:
• 2 vCPU / 4 GB ECC RAM / 60 GB NVMe / 1 IPv4 + /64 IPv6 / 4TB on 1Gbps port: $10/mo flat.
• 1 vCPU / 2 GB ECC RAM / 30 GB NVMe: $5/mo flat.
• Zero egress gouging. Native KVM virtualization.

No obsolete 400W dual-Xeon space heaters, no consumer desktop SSDs, and port 25 is closed by default to keep subnets pristine.

DM me for a 48-hour test instance or fio/iperf3 results.
```

---

## Operational Launch Checklist

- [ ] **Hardware Pre-Flight:** memtest86 48-hour burn-in completed on 256GB ECC RAM with zero errors.
- [ ] **Storage Verification:** fio 4k random write benchmark verifying >120,000 IOPS on RAID 10 NVMe array.
- [ ] **Power Envelope Check:** Kill-A-Watt or IPMI reading verifies system idle ≤ 110W, max load ≤ 225W.
- [ ] **Network ACL Enforcement:** Outbound TCP ports 25, 465, and 587 dropped at host iptables/nftables by default.
- [ ] **Automated Fraud Scoring:** MaxMind API linked to Blesta billing; risk threshold set to 15.
- [ ] **Offsite Backups:** Nightly ZFS snapshot replication to encrypted S3 bucket verified.
