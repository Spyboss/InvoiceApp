import { useEffect, useState } from 'react'
import { Button } from './ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Loader2, Download, RefreshCw } from 'lucide-react'
import { api, ApiError, downloadBlob, type InvoiceWithCustomer } from '../lib/api'

const TYPE_LABEL: Record<string, string> = {
  PROFORMA: 'Proforma',
  SALES_CASH: 'Sales · Cash',
  SALES_LEASING: 'Sales · Leasing',
}

const rs = (v: string) => `Rs. ${Number(v).toLocaleString('en-US', { minimumFractionDigits: 2 })}`

export function InvoiceList() {
  const [invoices, setInvoices] = useState<InvoiceWithCustomer[]>([])
  const [loading, setLoading] = useState(true)
  const [downloading, setDownloading] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = async () => {
    setLoading(true)
    setError(null)
    try {
      const r = await api.invoices()
      setInvoices(r.invoices)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Connection failed')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const download = async (id: string) => {
    setDownloading(id)
    setError(null)
    try {
      const { blob, filename } = await api.invoicePdf(id)
      downloadBlob(blob, filename)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Download failed')
    } finally {
      setDownloading(null)
    }
  }

  return (
    <Card className="w-full max-w-5xl mx-auto my-8">
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle>Invoices</CardTitle>
          <CardDescription>Every record in the system — re-download any PDF.</CardDescription>
        </div>
        <Button variant="outline" size="sm" onClick={load} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
        </Button>
      </CardHeader>
      <CardContent>
        {error && <p className="text-red-600 text-sm mb-4">{error}</p>}
        {loading ? (
          <p className="text-slate-500 text-sm flex items-center"><Loader2 className="mr-2 h-4 w-4 animate-spin" /> Loading…</p>
        ) : invoices.length === 0 ? (
          <p className="text-slate-500 text-sm">No invoices yet — create the first one.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-slate-500 border-b">
                  <th className="py-2 pr-4">Type</th>
                  <th className="py-2 pr-4">No.</th>
                  <th className="py-2 pr-4">Date</th>
                  <th className="py-2 pr-4">Customer</th>
                  <th className="py-2 pr-4 text-right">Price</th>
                  <th className="py-2 pr-4 text-right">Balance</th>
                  <th className="py-2">PDF</th>
                </tr>
              </thead>
              <tbody>
                {invoices.map((inv) => (
                  <tr key={inv.id} className="border-b last:border-0">
                    <td className="py-2 pr-4">{TYPE_LABEL[inv.invoiceType] ?? inv.invoiceType}</td>
                    <td className="py-2 pr-4 font-mono">{inv.invoiceNo}</td>
                    <td className="py-2 pr-4">{String(inv.date).slice(0, 10)}</td>
                    <td className="py-2 pr-4">{inv.customer?.name}</td>
                    <td className="py-2 pr-4 text-right">{rs(inv.price)}</td>
                    <td className="py-2 pr-4 text-right">{rs(inv.balance)}</td>
                    <td className="py-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => download(inv.id)}
                        disabled={downloading === inv.id}
                      >
                        {downloading === inv.id
                          ? <Loader2 className="h-4 w-4 animate-spin" />
                          : <Download className="h-4 w-4" />}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
