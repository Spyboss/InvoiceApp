import { Fragment, useEffect, useState } from 'react'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { Loader2, Download, RefreshCw, Wallet } from 'lucide-react'
import { api, ApiError, downloadBlob, type InvoiceWithCustomer, type Payment } from '../lib/api'
import type { PaymentMethod } from '@erp/shared'

const TYPE_LABEL: Record<string, string> = {
  PROFORMA: 'Proforma',
  SALES_CASH: 'Sales · Cash',
  SALES_LEASING: 'Sales · Leasing',
}

const METHOD_LABEL: Record<string, string> = {
  CASH: 'Cash', CHEQUE: 'Cheque', CARD: 'Card', BANK_TRANSFER: 'Bank', LEASING: 'Leasing',
}

const rs = (v: string) => `Rs. ${Number(v).toLocaleString('en-US', { minimumFractionDigits: 2 })}`
const when = (iso: string) => new Date(iso).toLocaleString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })

export function InvoiceList() {
  const [invoices, setInvoices] = useState<InvoiceWithCustomer[]>([])
  const [loading, setLoading] = useState(true)
  const [downloading, setDownloading] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  // payment panel
  const [openId, setOpenId] = useState<string | null>(null)
  const [payRows, setPayRows] = useState<Payment[]>([])
  const [payAmount, setPayAmount] = useState('')
  const [payMethod, setPayMethod] = useState<PaymentMethod>('CASH')
  const [payBusy, setPayBusy] = useState(false)
  const [payError, setPayError] = useState<string | null>(null)

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

  const togglePayments = async (id: string) => {
    if (openId === id) { setOpenId(null); return }
    setOpenId(id)
    setPayError(null)
    setPayRows([])
    try {
      const r = await api.payments(id)
      setPayRows(r.payments)
    } catch (e) {
      setPayError(e instanceof ApiError ? e.message : 'Failed to load payments')
    }
  }

  const addPayment = async (invoiceId: string) => {
    setPayBusy(true)
    setPayError(null)
    try {
      const r = await api.recordPayment({ invoiceId, amount: payAmount, method: payMethod })
      setInvoices((prev) => prev.map((i) => (i.id === invoiceId ? { ...i, balance: r.invoiceBalance } : i)))
      const h = await api.payments(invoiceId)
      setPayRows(h.payments)
      setPayAmount('')
    } catch (e) {
      setPayError(e instanceof ApiError ? e.message : 'Payment failed')
    } finally {
      setPayBusy(false)
    }
  }

  return (
    <Card className="w-full max-w-5xl mx-auto my-8">
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle>Invoices</CardTitle>
          <CardDescription>Every record in the system — re-download any PDF, record payments.</CardDescription>
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
                  <th className="py-2 pr-2">Payments</th>
                  <th className="py-2">PDF</th>
                </tr>
              </thead>
              <tbody>
                {invoices.map((inv) => (
                  <Fragment key={inv.id}>
                    <tr className="border-b">
                      <td className="py-2 pr-4">{TYPE_LABEL[inv.invoiceType] ?? inv.invoiceType}</td>
                      <td className="py-2 pr-4 font-mono">{inv.invoiceNo}</td>
                      <td className="py-2 pr-4">{String(inv.date).slice(0, 10)}</td>
                      <td className="py-2 pr-4">{inv.customer?.name}</td>
                      <td className="py-2 pr-4 text-right">{rs(inv.price)}</td>
                      <td className={`py-2 pr-4 text-right font-medium ${Number(inv.balance) > 0 ? 'text-amber-700' : 'text-green-700'}`}>
                        {Number(inv.balance) > 0 ? rs(inv.balance) : 'PAID'}
                      </td>
                      <td className="py-2 pr-2">
                        <Button variant="outline" size="sm" title="Payments" onClick={() => togglePayments(inv.id)}>
                          <Wallet className="h-4 w-4" />
                        </Button>
                      </td>
                      <td className="py-2">
                        <Button variant="outline" size="sm" title="PDF" onClick={() => download(inv.id)} disabled={downloading === inv.id}>
                          {downloading === inv.id
                            ? <Loader2 className="h-4 w-4 animate-spin" />
                            : <Download className="h-4 w-4" />}
                        </Button>
                      </td>
                    </tr>
                    {openId === inv.id && (
                      <tr className="bg-slate-50 border-b">
                        <td colSpan={8} className="py-3 px-2">
                          <div className="flex flex-wrap items-end gap-3">
                            <div className="space-y-1">
                              <Label className="text-xs">Amount (Rs)</Label>
                              <Input type="number" step="0.01" className="h-8 w-36 text-sm"
                                value={payAmount} onChange={(e) => setPayAmount(e.target.value)} />
                            </div>
                            <div className="space-y-1">
                              <Label className="text-xs">Method</Label>
                              <Select value={payMethod} onValueChange={(v) => setPayMethod(v as PaymentMethod)}>
                                <SelectTrigger className="h-8 w-36 text-sm"><SelectValue /></SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="CASH">Cash</SelectItem>
                                  <SelectItem value="CHEQUE">Cheque</SelectItem>
                                  <SelectItem value="CARD">Card</SelectItem>
                                  <SelectItem value="BANK_TRANSFER">Bank transfer</SelectItem>
                                  <SelectItem value="LEASING">Leasing</SelectItem>
                                </SelectContent>
                              </Select>
                            </div>
                            <Button size="sm" className="h-8" onClick={() => addPayment(inv.id)}
                              disabled={payBusy || !payAmount || Number(payAmount) <= 0}>
                              {payBusy ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : null} Record payment
                            </Button>
                            <span className="text-xs text-slate-500 ml-auto">
                              Outstanding: <strong>{rs(inv.balance)}</strong>
                            </span>
                          </div>
                          {payError && <p className="text-red-600 text-xs mt-2">{payError}</p>}
                          {payRows.length > 0 ? (
                            <table className="w-full text-xs mt-3">
                              <thead>
                                <tr className="text-left text-slate-500">
                                  <th className="py-1 pr-4">When</th>
                                  <th className="py-1 pr-4">Method</th>
                                  <th className="py-1 pr-4 text-right">Amount</th>
                                  <th className="py-1">Notes</th>
                                </tr>
                              </thead>
                              <tbody>
                                {payRows.map((p) => (
                                  <tr key={p.id} className="border-t border-slate-200">
                                    <td className="py-1 pr-4">{when(p.paidAt)}</td>
                                    <td className="py-1 pr-4">{METHOD_LABEL[p.method] ?? p.method}</td>
                                    <td className="py-1 pr-4 text-right">{rs(p.amount)}</td>
                                    <td className="py-1 text-slate-500">{p.notes ?? '—'}</td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          ) : (
                            !payError && <p className="text-xs text-slate-400 mt-2">No payments recorded yet.</p>
                          )}
                        </td>
                      </tr>
                    )}
                  </Fragment>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
