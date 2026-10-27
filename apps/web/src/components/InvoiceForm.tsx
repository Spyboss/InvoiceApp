import { useEffect, useState } from 'react'
import { useForm, Controller, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import * as z from 'zod'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { Textarea } from './ui/textarea'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { Loader2, CheckCircle2 } from 'lucide-react'
import { api, ApiError, downloadBlob, type InvoiceCreateInput, type Vehicle } from '../lib/api'

const MANUAL = '__manual__'

const invoiceSchema = z
  .object({
    invoice_type: z.enum(['PROFORMA', 'SALES_CASH', 'SALES_LEASING']),
    date: z.string().min(1, 'Date is required'),
    dealer: z.string().min(1, 'Dealer name is required'),
    customer_name: z.string().min(2, 'Customer name is required'),
    nic: z.string().optional(),
    phone: z.string().optional(),
    cust_addr: z.string().optional(),
    vehicle_id: z.string(),
    make: z.string().optional(),
    model: z.string().optional(),
    vehicle_type: z.string().optional(),
    engine: z.string().optional(),
    chassis: z.string().optional(),
    color: z.string().optional(),
    yom: z.string().optional(),
    finance_company: z.string().optional(),
    finance_address: z.string().optional(),
    delivery: z.string().optional(),
    price: z
      .string()
      .transform((v) => parseFloat(v) || 0)
      .refine((v) => v > 0, 'Total price is required'),
    down: z
      .string()
      .transform((v) => parseFloat(v) || 0)
      .refine((v) => v >= 0, 'Down payment cannot be negative'),
  })
  .superRefine((values, ctx) => {
    if (values.vehicle_id === MANUAL && !values.model?.trim()) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['model'], message: 'Model is required' })
    }
  })

type FormInput = z.input<typeof invoiceSchema>
type FormOutput = z.output<typeof invoiceSchema>

export function InvoiceForm() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<string | null>(null)
  const [stock, setStock] = useState<Vehicle[]>([])

  const { register, control, handleSubmit, watch, reset, formState: { errors } } = useForm<FormInput, unknown, FormOutput>({
    // @hookform/resolvers v3 can't infer the transformed output type — cast is the documented workaround
    resolver: zodResolver(invoiceSchema) as Resolver<FormInput, unknown, FormOutput>,
    defaultValues: {
      invoice_type: 'SALES_CASH',
      date: new Date().toISOString().slice(0, 10),
      dealer: 'Gunawardhana Enterprises, Beliatta Road, Tangalle',
      vehicle_id: MANUAL,
      vehicle_type: 'PICKUP',
      yom: String(new Date().getFullYear()),
      finance_company: 'Vallibel Finance PLC',
      finance_address: 'No. 54, Beliatta Road, Tangalle',
      price: '' as unknown as string,
      down: '' as unknown as string,
    },
  })

  useEffect(() => {
    api.vehicles('AVAILABLE').then((r) => setStock(r.vehicles)).catch(() => setStock([]))
  }, [])

  const invoiceType = watch('invoice_type')
  const vehicleId = watch('vehicle_id')
  const isLeasing = invoiceType === 'SALES_LEASING'
  const showFinance = isLeasing || invoiceType === 'PROFORMA'
  const selected = stock.find((v) => v.id === vehicleId)

  const onSubmit = async (values: FormOutput) => {
    setLoading(true)
    setError(null)
    setDone(null)
    try {
      // 1. resolve customer: match by name, create when new
      const name = values.customer_name.trim()
      const { customers } = await api.customers(name)
      const match = customers.find((c) => c.name.trim().toLowerCase() === name.toLowerCase())
      const customer =
        match ??
        (await api.createCustomer({ name, nic: values.nic, phone: values.phone, address: values.cust_addr })).customer

      // 2. build payload — stock link OR manual snapshot
      const payload: InvoiceCreateInput = {
        invoiceType: values.invoice_type,
        date: values.date,
        customerId: customer.id,
        dealer: values.dealer,
        price: values.price!.toFixed(2),
        down: values.down!.toFixed(2),
        ...(selected
          ? { vehicleId: selected.id }
          : {
              make: values.make,
              model: values.model,
              vehicleType: values.vehicle_type,
              engineNo: values.engine,
              chassisNo: values.chassis,
              color: values.color,
              ...(values.yom ? { yom: Number(values.yom) } : {}),
            }),
        ...(showFinance && values.finance_company
          ? { financeCompany: values.finance_company, financeAddress: values.finance_address }
          : {}),
        ...(isLeasing && values.delivery ? { delivery: values.delivery } : {}),
      }

      // 3. create record, then pull the PDF
      const { invoice } = await api.createInvoice(payload)
      const { blob, filename } = await api.invoicePdf(invoice.id)
      downloadBlob(blob, filename)
      setDone(`Invoice ${invoice.invoiceNo} created — PDF downloaded.`)
      reset({ customer_name: '', nic: '', phone: '', cust_addr: '', make: '', model: '', engine: '', chassis: '', color: '', delivery: '' })
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Connection failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Card className="w-full max-w-4xl mx-auto my-8">
      <CardHeader>
        <CardTitle>Invoice Generator</CardTitle>
        <CardDescription>Create sales or proforma invoices — record first, PDF after.</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <div className="space-y-2">
              <Label>Invoice Type</Label>
              <Controller
                name="invoice_type"
                control={control}
                render={({ field }) => (
                  <Select onValueChange={field.onChange} defaultValue={field.value}>
                    <SelectTrigger><SelectValue placeholder="Select type" /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="SALES_CASH">Sales (Cash)</SelectItem>
                      <SelectItem value="SALES_LEASING">Sales (Leasing)</SelectItem>
                      <SelectItem value="PROFORMA">Proforma</SelectItem>
                    </SelectContent>
                  </Select>
                )}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="date">Invoice Date</Label>
              <Input type="date" {...register('date')} />
            </div>
            <div className="space-y-2">
              <Label>Stock Vehicle</Label>
              <Controller
                name="vehicle_id"
                control={control}
                render={({ field }) => (
                  <Select onValueChange={field.onChange} defaultValue={field.value}>
                    <SelectTrigger><SelectValue placeholder="Select vehicle" /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value={MANUAL}>— Manual entry —</SelectItem>
                      {stock.map((v) => (
                        <SelectItem key={v.id} value={v.id}>
                          {v.model} · {v.chassisNo}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 border-t pt-4">
            <div className="space-y-2">
              <Label htmlFor="customer_name">Customer Name</Label>
              <Input {...register('customer_name')} />
              {errors.customer_name && <p className="text-red-500 text-sm">{errors.customer_name.message}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="nic">Customer NIC</Label>
              <Input {...register('nic')} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="phone">Contact No</Label>
              <Input {...register('phone')} placeholder="07XXXXXXXX" />
            </div>
            <div className="space-y-2 md:col-span-2">
              <Label htmlFor="cust_addr">Customer Address</Label>
              <Textarea {...register('cust_addr')} />
            </div>
          </div>

          {showFinance && (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6 border-t pt-4 bg-slate-50 p-4 rounded-md">
              <div className="space-y-2">
                <Label htmlFor="finance_company">Finance Company</Label>
                <Input {...register('finance_company')} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="finance_address">Finance Address</Label>
                <Input {...register('finance_address')} />
              </div>
              {isLeasing && (
                <div className="space-y-2 md:col-span-2">
                  <Label htmlFor="delivery">Delivery Address (Leasing)</Label>
                  <Textarea {...register('delivery')} />
                </div>
              )}
            </div>
          )}

          {selected ? (
            <div className="border-t pt-4 grid grid-cols-2 md:grid-cols-4 gap-4 text-sm bg-slate-50 p-4 rounded-md">
              <div><span className="text-slate-500 block">Model</span>{selected.model}</div>
              <div><span className="text-slate-500 block">Type</span>{selected.type}</div>
              <div><span className="text-slate-500 block">Engine No</span>{selected.engineNo}</div>
              <div><span className="text-slate-500 block">Chassis No</span>{selected.chassisNo}</div>
              <div><span className="text-slate-500 block">Colour</span>{selected.color}</div>
              <div><span className="text-slate-500 block">YOM</span>{selected.yom}</div>
            </div>
          ) : (
            <div className="border-t pt-4 grid grid-cols-1 md:grid-cols-4 gap-4">
              <div className="space-y-2">
                <Label htmlFor="make">Make</Label>
                <Input {...register('make')} placeholder="TAILG" />
              </div>
              <div className="space-y-2">
                <Label htmlFor="model">Model</Label>
                <Input {...register('model')} placeholder="APE Classic DX" />
                {errors.model && <p className="text-red-500 text-sm">{errors.model.message}</p>}
              </div>
              <div className="space-y-2">
                <Label htmlFor="vehicle_type">Vehicle Type</Label>
                <Input {...register('vehicle_type')} placeholder="PICKUP" />
              </div>
              <div className="space-y-2">
                <Label htmlFor="color">Colour</Label>
                <Input {...register('color')} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="engine">Engine No</Label>
                <Input {...register('engine')} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="chassis">Chassis No</Label>
                <Input {...register('chassis')} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="yom">Year of Manufacture</Label>
                <Input {...register('yom')} />
              </div>
            </div>
          )}

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 border-t pt-4">
            <div className="space-y-2">
              <Label htmlFor="price">Total Price (Rs)</Label>
              <Input type="number" step="0.01" {...register('price')} />
              {errors.price && <p className="text-red-500 text-sm">{errors.price.message}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="down">Down Payment (Rs)</Label>
              <Input type="number" step="0.01" {...register('down')} />
              {errors.down && <p className="text-red-500 text-sm">{errors.down.message}</p>}
            </div>
          </div>

          {error && <p className="text-red-600 text-sm">{error}</p>}
          {done && (
            <p className="text-green-700 text-sm flex items-center gap-2">
              <CheckCircle2 className="h-4 w-4" /> {done}
            </p>
          )}

          <Button type="submit" className="w-full" disabled={loading}>
            {loading ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Create Invoice & Download PDF
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}
