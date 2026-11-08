export const money = (cents: number | null | undefined, digits = 2) =>
  cents == null ? '—' : '$' + (cents / 100).toLocaleString('en-US', { minimumFractionDigits: digits, maximumFractionDigits: digits })

export const pct = (v: number) => (v * 100).toFixed(1) + '%'

export const int = (v: number) => v.toLocaleString('en-US')

export const tierLabel: Record<string, string> = {
  reference: 'Reference', good: 'Good', anti: 'Anti-example', early: 'Early', nodata: 'No data',
}

export const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`
