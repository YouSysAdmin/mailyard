// Saving something the page already holds as a file, and turning rows
// into CSV.
//
// Both exports in this console are DOCUMENTS rather than streams: the
// server answers in the same JSON envelope as every other endpoint and
// the page builds the file. That was written twice - the project data
// export and now the audit log - and the blob/objectURL/revoke dance is
// exactly the kind of thing where the second copy forgets to revoke.

// downloadText saves text as a file with the given name.
export function downloadText(filename: string, mime: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: mime }))
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

// dateStamp is the date part of an ISO timestamp, for a filename.
//
// Date only, so repeated exports on one day overwrite rather than pile
// up as "(1)", "(2)" copies in the downloads folder.
export function dateStamp(): string {
  return new Date().toISOString().slice(0, 10)
}

// toCSV writes RFC 4180 rows: quotes doubled, every field quoted, CRLF
// line endings.
//
// Every field quoted rather than only the ones that need it - a rule
// with no exceptions cannot be applied wrongly, and the file is the
// same size to a spreadsheet either way.
export function toCSV(headers: string[], rows: unknown[][]): string {
  const out = [headers.map(cell).join(',')]
  for (const row of rows) out.push(row.map(cell).join(','))
  // A trailing CRLF, so the last line is terminated like the others.
  return out.join('\r\n') + '\r\n'
}

function cell(value: unknown): string {
  const raw = value === null || value === undefined ? '' : String(value)
  return '"' + defuse(raw).replace(/"/g, '""') + '"'
}

// defuse stops a spreadsheet reading a field as a FORMULA.
//
// An audit row carries text somebody else wrote - a request path, an
// actor's address, the detail line - and Excel, LibreOffice and Sheets
// all evaluate a cell that opens with = + - @ or a control character,
// quoted or not. So `=1+1` is arithmetic and `=HYPERLINK(...)` or a DDE
// formula is worse than that. A leading apostrophe is the standard fix
// and the one every spreadsheet understands: the cell reads as text and
// the apostrophe is not part of the value.
function defuse(raw: string): string {
  return /^[=+\-@\t\r]/.test(raw) ? "'" + raw : raw
}
