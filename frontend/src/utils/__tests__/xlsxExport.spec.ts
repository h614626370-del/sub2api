import { describe, expect, it } from 'vitest'

describe('SheetJS usage export compatibility', () => {
  it('writes appended usage pages and preserves string and numeric cells', async () => {
    const XLSX = await import('xlsx')
    const headers = ['Account', 'Tokens', 'Cost', 'Request ID']
    const firstPage = [['Account <A> & B', 123, '0.001200', '=literal-not-formula']]
    const secondPage = [['Account\nsecond line', 0, '0.000000', 'req-2']]
    const sheet = XLSX.utils.aoa_to_sheet([headers])
    XLSX.utils.sheet_add_aoa(sheet, firstPage, { origin: -1 })
    XLSX.utils.sheet_add_aoa(sheet, secondPage, { origin: -1 })
    const workbook = XLSX.utils.book_new()
    XLSX.utils.book_append_sheet(workbook, sheet, 'Usage')

    const bytes = XLSX.write(workbook, { bookType: 'xlsx', type: 'array' })
    expect(bytes.byteLength).toBeGreaterThan(0)
    const restored = XLSX.read(bytes, { type: 'array' })
    expect(restored.SheetNames).toEqual(['Usage'])
    expect(XLSX.utils.sheet_to_json(restored.Sheets.Usage, { header: 1 })).toEqual([
      headers, ...firstPage, ...secondPage,
    ])
    expect(restored.Sheets.Usage.D2.t).toBe('s')
    expect(restored.Sheets.Usage.D2.f).toBeUndefined()
  })
})
