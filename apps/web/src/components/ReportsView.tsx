import { useState } from 'react'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Loader2, FileText } from 'lucide-react'
import { api, ApiError, downloadBlob } from '../lib/api'

const monthStart = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`
}
const today = () => new Date().toISOString().slice(0, 10)

export function ReportsView() {
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [from, setFrom] = useState(monthStart())
  const [to, setTo] = useState(today())

  const run = async (name: 'sales' | 'stock' | 'receivables', params: Record<string, string | undefined> = {}) => {
    setBusy(name)
    setError(null)
    try {
      const { blob, filename } = await api.reportPdf(name, params)
      downloadBlob(blob, filename)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Report failed')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="w-full max-w-5xl mx-auto my-8 space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>Reports</CardTitle>
          <CardDescription>Printable A4 PDFs — multi-page with repeating headers and totals.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div className="border rounded-md p-4 space-y-3">
              <div className="flex items-center gap-2 font-semibold"><FileText className="h-4 w-4" /> Sales report</div>
              <p className="text-xs text-slate-500">Invoices issued in a period, with totals.</p>
              <div className="grid grid-cols-2 gap-2">
                <div className="space-y-1"><Label className="text-xs">From</Label>
                  <Input type="date" value={from} onChange={(e) => setFrom(e.target.value)} /></div>
                <div className="space-y-1"><Label className="text-xs">To</Label>
                  <Input type="date" value={to} onChange={(e) => setTo(e.target.value)} /></div>
              </div>
              <Button size="sm" className="w-full" disabled={busy === 'sales'} onClick={() => run('sales', { from, to })}>
                {busy === 'sales' ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : null} Download PDF
              </Button>
            </div>

            <div className="border rounded-md p-4 space-y-3">
              <div className="flex items-center gap-2 font-semibold"><FileText className="h-4 w-4" /> Stock report</div>
              <p className="text-xs text-slate-500">Vehicles by status + parts inventory, low-stock flags.</p>
              <div className="h-[52px]" />
              <Button size="sm" className="w-full" disabled={busy === 'stock'} onClick={() => run('stock')}>
                {busy === 'stock' ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : null} Download PDF
              </Button>
            </div>

            <div className="border rounded-md p-4 space-y-3">
              <div className="flex items-center gap-2 font-semibold"><FileText className="h-4 w-4" /> Receivables</div>
              <p className="text-xs text-slate-500">Invoices still carrying a balance, with total owed.</p>
              <div className="h-[52px]" />
              <Button size="sm" className="w-full" disabled={busy === 'receivables'} onClick={() => run('receivables')}>
                {busy === 'receivables' ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : null} Download PDF
              </Button>
            </div>
          </div>
          {error && <p className="text-red-600 text-sm">{error}</p>}
        </CardContent>
      </Card>
    </div>
  )
}
