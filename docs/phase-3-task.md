# Phase 3 Tasks — Excel expense reports

Goal: send a formatted XLSX file in Telegram with date-grouped expenses, total spending, and a category pie chart. XLSX replaces the earlier CSV scope. No expense ID, income, or remaining balance is shown. Google Sheets integration remains undecided.

- [x] Add `/export` for this month and `/export YYYY-MM-DD YYYY-MM-DD` for an inclusive custom range.
- [x] Validate date ranges; cap exports at 366 days, 2,000 expenses, and 5 MB.
- [x] Export only the requesting active, allowed user's confirmed, non-deleted expenses.
- [x] Snapshot rows at request time; enqueue the snapshot atomically with update deduplication.
- [x] Limit requests to one per user per five minutes, one pending export per user, and 20 globally queued exports.
- [x] Generate workbooks in the existing serial delivery worker, outside the webhook transaction.
- [x] Format Date, Category, Notes, Amount columns with grouped dates, colored categories, and rupiah amounts.
- [x] Show total spending and a native category pie chart with percentage labels and legend.
- [x] Add a supporting category summary sheet; use formulas for totals and test recalculation.
- [x] Treat user text as strings, preserve Unicode, and guard Excel's numeric precision limits.
- [x] Send the XLSX as a Telegram document with deterministic filename and retry/restart recovery.
- [x] Clear snapshot payloads on successful delivery, terminal failure, or blacklist suppression; generate in memory without permanent files.
- [x] Test ownership, filters, edited/deleted records, boundaries, caps, deduplication, blocked users, delivery, workbook content, and chart structure.
- [x] Generate a sample workbook, check totals, render the output, and inspect it visually.
- [x] Run full unit/integration checks, vet, and builds; update HLD, help, and usage docs.
- [ ] Deploy and verify an XLSX download from the production bot.

Validation: all unit/integration tests, formatting, vet, and Go builds passed. A 2,000-row local benchmark used approximately 21 MB peak resident memory and took about 29 ms on Apple M4 Pro (not a Render capacity guarantee). Both sheets were rendered and visually inspected; exact category formulas and amount changes were also checked in the spreadsheet engine.

- [ ] Complete Docker image verification once Docker Hub metadata requests succeed; the local build is currently blocked on registry access.
