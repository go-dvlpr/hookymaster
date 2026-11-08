# Test data

Synthetic data for development and the demo. Numbers are generated, not real, but
follow the SockLock funnel: CPM ~$12, CPA ~$50.

## Files

| File | What it is |
|---|---|
| `seed/projects.json` | Three projects (O12, O15, O18) with name and description, used by `seed` |
| `scripts/O12.md`, `O15.md`, `O18.md` | Example scripts of the three concepts, for reading only |
| `meta_ads/hooks_import.csv` | Known hook texts for all three projects, 38 hooks |
| `meta_ads/ads_manager_export_2026-10.csv` | Ads Manager export for October 2026, 51 rows |
| `meta_ads/ads_manager_export_missing_column.csv` | Same format without `3-second video plays`; must be rejected |

Four hooks are in `hooks_import.csv` but not in the export, because they were never
launched: `O12_H14`, `O12_H15`, `O15_H11`, `O18_H09`.

Totals for the US hooks: 4.99M impressions, $61K spend, CPM $12.24, hook rate 28.4%,
CTR 1.40%, CPA $48.53. CTR is a bit above the account average because these are
modifications of winners.

## Hook profiles

Each hook was generated from one profile:

| Profile | Hook rate | CTR | Click → purchase | Impressions | Expected tier |
|---|---|---|---|---|---|
| winner | 31–36% | 1.45–1.60% | 1.95–2.15% | 180–320K | Reference |
| good | 27–31% | 1.30–1.45% | 1.85–2.00% | 120–250K | Reference / Good |
| average | 22–26% | 1.10–1.25% | 1.90–2.05% | 80–200K | Good / Anti-example |
| weak | 13–18% | 0.75–0.95% | 1.60–1.80% | 40–120K | Early signal |
| clickbait | 34–40% | 1.90–2.20% | 0.85–1.00% | 100–180K | Anti-example |
| noise | 38–45% | 2.00–2.50% | 0–2.5% | 1.5–2.8K | No data |

## Traps in the export

| Row | Trap | Expected behavior |
|---|---|---|
| Winners and good hooks | Same ad name in two ad sets (`US_Broad_25-54`, `US_Women_25-44`) | Summed into one hook |
| `US_O12_H05`, `O12_H16`, `O15_H09`, `O18_H07` | Great rates on ~2K impressions | No data |
| `US_O12_H06`, `O12_H11`, `O15_H06`, `O18_H04` | Best hook rate and CTR, CPA $60–66 | Anti-example |
| Rows with 0 purchases | `Purchases` and `Cost per purchase (USD)` are empty | Read as 0, CPA undefined |
| `US_O12_H99` | Valid name, number the project does not have | Listed as "no hook yet"; **New hook** creates H99 without text |
| `Socks_magnet_final_v2_NEW` | No naming convention | Listed as unrecognized; matches after setting this ad name on a hook |
| `us_o15_h03` | Lower case variant of `US_O15_H03` | Matched after normalization |
| `UK_O12_H01`, `UK_O12_H02` | Same hooks, UK market | Ignored by the US project, counted as "other markets" |
| `CPM (cost per 1,000 impressions) (USD)` header | Comma inside a quoted column name | Parsed by a real CSV parser |
