// Big numbers, short.
//
// A dashboard is scanned rather than read, and "1.2M" is one glance
// where "1,204,853" is three. Under ten thousand it stays exact, because
// that is a range where the digits still mean something to somebody
// watching their own sending.
export function formatNumber(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 10_000) return (n / 1_000).toFixed(1) + 'K'

  return n.toLocaleString()
}
