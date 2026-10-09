import { useEffect, useState } from 'react'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { Loader2, Plus, ArrowDownToLine, ArrowUpFromLine } from 'lucide-react'
import { api, ApiError, type Part, type Vehicle } from '../lib/api'

const rs = (v: string) => `Rs. ${Number(v).toLocaleString('en-US', { minimumFractionDigits: 2 })}`
const STATUS_STYLE: Record<string, string> = {
  AVAILABLE: 'text-green-700 bg-green-50',
  RESERVED: 'text-amber-700 bg-amber-50',
  SOLD: 'text-slate-600 bg-slate-100',
  DELIVERED: 'text-sky-700 bg-sky-50',
}

type Tab = 'vehicles' | 'parts'

export function StockView() {
  const [tab, setTab] = useState<Tab>('vehicles')
  return (
    <div className="w-full max-w-5xl mx-auto my-8 space-y-4">
      <div className="flex gap-2">
        <Button variant={tab === 'vehicles' ? 'default' : 'outline'} size="sm" onClick={() => setTab('vehicles')}>
          Vehicles
        </Button>
        <Button variant={tab === 'parts' ? 'default' : 'outline'} size="sm" onClick={() => setTab('parts')}>
          Parts
        </Button>
      </div>
      {tab === 'vehicles' ? <VehicleStock /> : <PartStock />}
    </div>
  )
}

// ---------------------------------------------------------------- vehicles
function VehicleStock() {
  const [vehicles, setVehicles] = useState<Vehicle[]>([])
  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState<string>('')
  const [showForm, setShowForm] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [form, setForm] = useState({
    chassisNo: '', engineNo: '', model: '', type: 'PICKUP', color: '',
    yom: String(new Date().getFullYear()), cost: '', price: '',
  })

  const load = async () => {
    setLoading(true)
    setError(null)
    try {
      const r = await api.vehicles(status ? (status as never) : undefined)
      setVehicles(r.vehicles)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Connection failed')
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => { load() }, [status])

  const submit = async () => {
    setSaving(true)
    setError(null)
    try {
      await api.createVehicle({
        ...form,
        yom: Number(form.yom),
        cost: form.cost || '0',
        price: form.price || '0',
      })
      setShowForm(false)
      setForm({ ...form, chassisNo: '', engineNo: '', color: '', cost: '', price: '' })
      await load()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle>Vehicle Stock</CardTitle>
          <CardDescription>{vehicles.length} unit(s) shown</CardDescription>
        </div>
        <div className="flex items-center gap-2">
          <Select value={status} onValueChange={setStatus}>
            <SelectTrigger className="w-40"><SelectValue placeholder="All statuses" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="">All statuses</SelectItem>
              <SelectItem value="AVAILABLE">Available</SelectItem>
              <SelectItem value="RESERVED">Reserved</SelectItem>
              <SelectItem value="SOLD">Sold</SelectItem>
              <SelectItem value="DELIVERED">Delivered</SelectItem>
            </SelectContent>
          </Select>
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="mr-1 h-4 w-4" /> Receive stock
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {showForm && (
          <div className="border rounded-md p-4 mb-4 grid grid-cols-1 md:grid-cols-4 gap-3 bg-slate-50">
            <div className="space-y-1"><Label>Chassis No *</Label>
              <Input value={form.chassisNo} onChange={(e) => setForm({ ...form, chassisNo: e.target.value })} /></div>
            <div className="space-y-1"><Label>Engine No *</Label>
              <Input value={form.engineNo} onChange={(e) => setForm({ ...form, engineNo: e.target.value })} /></div>
            <div className="space-y-1"><Label>Model *</Label>
              <Input value={form.model} onChange={(e) => setForm({ ...form, model: e.target.value })} placeholder="APE Classic DX" /></div>
            <div className="space-y-1"><Label>Type</Label>
              <Input value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })} /></div>
            <div className="space-y-1"><Label>Colour</Label>
              <Input value={form.color} onChange={(e) => setForm({ ...form, color: e.target.value })} /></div>
            <div className="space-y-1"><Label>YOM</Label>
              <Input value={form.yom} onChange={(e) => setForm({ ...form, yom: e.target.value })} /></div>
            <div className="space-y-1"><Label>Cost (Rs)</Label>
              <Input type="number" value={form.cost} onChange={(e) => setForm({ ...form, cost: e.target.value })} /></div>
            <div className="space-y-1"><Label>Price (Rs)</Label>
              <Input type="number" value={form.price} onChange={(e) => setForm({ ...form, price: e.target.value })} /></div>
            <div className="md:col-span-4 flex gap-2 justify-end">
              <Button variant="outline" size="sm" onClick={() => setShowForm(false)}>Cancel</Button>
              <Button size="sm" onClick={submit} disabled={saving || !form.chassisNo || !form.engineNo || !form.model}>
                {saving ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : null} Save vehicle
              </Button>
            </div>
          </div>
        )}
        {error && <p className="text-red-600 text-sm mb-3">{error}</p>}
        {loading ? (
          <p className="text-slate-500 text-sm flex items-center"><Loader2 className="mr-2 h-4 w-4 animate-spin" /> Loading…</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-slate-500 border-b">
                  <th className="py-2 pr-3">Chassis</th><th className="py-2 pr-3">Model</th>
                  <th className="py-2 pr-3">Type</th><th className="py-2 pr-3">YOM</th>
                  <th className="py-2 pr-3">Status</th>
                  <th className="py-2 pr-3 text-right">Cost</th><th className="py-2 text-right">Price</th>
                </tr>
              </thead>
              <tbody>
                {vehicles.map((v) => (
                  <tr key={v.id} className="border-b last:border-0">
                    <td className="py-2 pr-3 font-mono text-xs">{v.chassisNo}</td>
                    <td className="py-2 pr-3">{v.model}</td>
                    <td className="py-2 pr-3">{v.type}</td>
                    <td className="py-2 pr-3">{v.yom}</td>
                    <td className="py-2 pr-3">
                      <span className={`text-xs px-2 py-0.5 rounded-full ${STATUS_STYLE[v.status] ?? ''}`}>{v.status}</span>
                    </td>
                    <td className="py-2 pr-3 text-right">{rs(v.cost)}</td>
                    <td className="py-2 text-right">{rs(v.price)}</td>
                  </tr>
                ))}
                {vehicles.length === 0 && (
                  <tr><td colSpan={7} className="py-4 text-slate-500 text-sm">No vehicles yet.</td></tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// ------------------------------------------------------------------- parts
function PartStock() {
  const [parts, setParts] = useState<Part[]>([])
  const [loading, setLoading] = useState(true)
  const [q, setQ] = useState('')
  const [showForm, setShowForm] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<string | null>(null)
  const [form, setForm] = useState({ sku: '', name: '', category: '', cost: '', price: '', reorderLevel: '5' })
  // inline movement form: which part row is acting, and with what qty
  const [movePart, setMovePart] = useState<string | null>(null)
  const [moveQty, setMoveQty] = useState('')
  const [moveDir, setMoveDir] = useState<'IN' | 'OUT'>('IN')
  const [moveSaving, setMoveSaving] = useState(false)

  const load = async () => {
    setLoading(true)
    setError(null)
    try {
      const r = await api.parts(q || undefined)
      setParts(r.parts)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Connection failed')
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    const t = setTimeout(load, 250) // debounce search
    return () => clearTimeout(t)
  }, [q])

  const submit = async () => {
    setSaving(true)
    setError(null)
    try {
      await api.createPart({
        sku: form.sku, name: form.name, category: form.category || undefined,
        cost: form.cost || '0', price: form.price || '0', reorderLevel: form.reorderLevel || '0',
      })
      setShowForm(false)
      setForm({ sku: '', name: '', category: '', cost: '', price: '', reorderLevel: '5' })
      await load()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  const submitMovement = async () => {
    if (!movePart || !moveQty) return
    setMoveSaving(true)
    setError(null)
    setDone(null)
    try {
      const qty = moveDir === 'IN' ? Math.abs(Number(moveQty)) : -Math.abs(Number(moveQty))
      const r = await api.createPartMovement(movePart, { qty, reason: moveDir === 'IN' ? 'PURCHASE' : 'SERVICE_JOB' })
      const part = parts.find((p) => p.id === movePart)
      setDone(`${part?.name ?? 'Part'}: ${qty > 0 ? '+' : ''}${qty} → on hand ${r.qtyOnHand}`)
      setMovePart(null)
      setMoveQty('')
      await load()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Movement failed')
    } finally {
      setMoveSaving(false)
    }
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle>Parts Inventory</CardTitle>
          <CardDescription>{parts.length} item(s) — red quantities are at/below reorder</CardDescription>
        </div>
        <div className="flex items-center gap-2">
          <Input placeholder="Search name / SKU…" value={q} onChange={(e) => setQ(e.target.value)} className="w-48" />
          <Button size="sm" onClick={() => setShowForm(!showForm)}>
            <Plus className="mr-1 h-4 w-4" /> New part
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {showForm && (
          <div className="border rounded-md p-4 mb-4 grid grid-cols-1 md:grid-cols-5 gap-3 bg-slate-50">
            <div className="space-y-1"><Label>SKU *</Label>
              <Input value={form.sku} onChange={(e) => setForm({ ...form, sku: e.target.value })} /></div>
            <div className="space-y-1 md:col-span-2"><Label>Name *</Label>
              <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></div>
            <div className="space-y-1"><Label>Category</Label>
              <Input value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value })} /></div>
            <div className="space-y-1"><Label>Reorder level</Label>
              <Input type="number" value={form.reorderLevel} onChange={(e) => setForm({ ...form, reorderLevel: e.target.value })} /></div>
            <div className="space-y-1"><Label>Cost (Rs)</Label>
              <Input type="number" value={form.cost} onChange={(e) => setForm({ ...form, cost: e.target.value })} /></div>
            <div className="space-y-1"><Label>Price (Rs)</Label>
              <Input type="number" value={form.price} onChange={(e) => setForm({ ...form, price: e.target.value })} /></div>
            <div className="md:col-span-5 flex gap-2 justify-end">
              <Button variant="outline" size="sm" onClick={() => setShowForm(false)}>Cancel</Button>
              <Button size="sm" onClick={submit} disabled={saving || !form.sku || !form.name}>
                {saving ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : null} Save part
              </Button>
            </div>
          </div>
        )}
        {error && <p className="text-red-600 text-sm mb-3">{error}</p>}
        {done && <p className="text-green-700 text-sm mb-3">{done}</p>}
        {loading ? (
          <p className="text-slate-500 text-sm flex items-center"><Loader2 className="mr-2 h-4 w-4 animate-spin" /> Loading…</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-slate-500 border-b">
                  <th className="py-2 pr-3">SKU</th><th className="py-2 pr-3">Name</th>
                  <th className="py-2 pr-3 text-right">Qty</th><th className="py-2 pr-3 text-right">Reorder</th>
                  <th className="py-2 pr-3 text-right">Price</th><th className="py-2 text-right">Movement</th>
                </tr>
              </thead>
              <tbody>
                {parts.map((p) => {
                  const low = Number(p.qtyOnHand) <= Number(p.reorderLevel)
                  const acting = movePart === p.id
                  return (
                    <tr key={p.id} className="border-b last:border-0">
                      <td className="py-2 pr-3 font-mono text-xs">{p.sku}</td>
                      <td className="py-2 pr-3">{p.name}</td>
                      <td className={`py-2 pr-3 text-right ${low ? 'text-red-600 font-semibold' : ''}`}>{Number(p.qtyOnHand)}</td>
                      <td className="py-2 pr-3 text-right text-slate-500">{Number(p.reorderLevel)}</td>
                      <td className="py-2 pr-3 text-right">{rs(p.price)}</td>
                      <td className="py-2 text-right whitespace-nowrap">
                        {acting ? (
                          <span className="inline-flex items-center gap-1">
                            <Input
                              type="number" min="1" autoFocus placeholder="qty"
                              value={moveQty}
                              onChange={(e) => setMoveQty(e.target.value)}
                              className="h-7 w-20 text-xs"
                            />
                            <Button size="sm" className="h-7 px-2 text-xs" onClick={submitMovement} disabled={moveSaving || !moveQty}>
                              {moveSaving ? <Loader2 className="h-3 w-3 animate-spin" /> : moveDir === 'IN' ? 'Receive' : 'Issue'}
                            </Button>
                            <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => setMovePart(null)}>✕</Button>
                          </span>
                        ) : (
                          <span className="inline-flex gap-1">
                            <Button size="sm" variant="outline" className="h-7 px-2 text-xs"
                              onClick={() => { setMovePart(p.id); setMoveDir('IN'); setMoveQty('') }}>
                              <ArrowDownToLine className="h-3 w-3" />
                            </Button>
                            <Button size="sm" variant="outline" className="h-7 px-2 text-xs"
                              onClick={() => { setMovePart(p.id); setMoveDir('OUT'); setMoveQty('') }}>
                              <ArrowUpFromLine className="h-3 w-3" />
                            </Button>
                          </span>
                        )}
                      </td>
                    </tr>
                  )
                })}
                {parts.length === 0 && (
                  <tr><td colSpan={6} className="py-4 text-slate-500 text-sm">No parts yet — add the first item.</td></tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
