import { useState } from "react"
import { useForm, Controller } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import * as z from "zod"
import axios from "axios"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Label } from "./ui/label"
import { Textarea } from "./ui/textarea"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select"
import { Loader2 } from "lucide-react"

const invoiceSchema = z.object({
  invoice_type: z.enum(["SALES-CASH", "SALES-LEASING", "PROFORMA"]),
  dealer: z.string().min(1, "Dealer name is required"),
  dealer_contact: z.string().optional(),
  finance_company: z.string().optional(),
  finance_address: z.string().optional(),
  customer: z.string().min(1, "Customer name is required"),
  nic: z.string().optional(),
  cust_addr: z.string().optional(),
  delivery: z.string().optional(),
  model: z.string().min(1, "Model is required"),
  engine: z.string().optional(),
  chassis: z.string().optional(),
  color: z.string().optional(),
  price: z.string().transform((val) => parseFloat(val) || 0),
  down: z.string().transform((val) => parseFloat(val) || 0),
})

type InvoiceFormValues = z.infer<typeof invoiceSchema>

const defaultValues: Partial<InvoiceFormValues> = {
  invoice_type: "SALES-CASH",
  dealer: "Gunawardhana Enterprises, Beliatta Road, Tangalle",
  dealer_contact: "077 8318061 / 077 8525428",
  finance_company: "Vallibel Finance PLC",
  finance_address: "No. 54, Beliatta Road, Tangalle",
  model: "APE AUTO DX PASSENGER (Diesel)",
  price: 0,
  down: 0,
}

export function InvoiceForm() {
  const [loading, setLoading] = useState(false)
  // const [downloadUrl, setDownloadUrl] = useState<string | null>(null)

  const { register, control, handleSubmit, watch, formState: { errors } } = useForm<any>({
    resolver: zodResolver(invoiceSchema),
    defaultValues,
  })

  const invoiceType = watch("invoice_type")
  const isLeasing = invoiceType === "SALES-LEASING"

  const onSubmit = async (data: any) => {
    setLoading(true)
    // setDownloadUrl(null)
    try {
      const response = await axios.post(`/api/invoices/${data.invoice_type}`, data, {
        responseType: 'blob',
      })
      
      const url = window.URL.createObjectURL(new Blob([response.data]))
      const link = document.createElement('a')
      link.href = url
      const contentDisposition = response.headers['content-disposition']
      let fileName = 'invoice.pdf'
      if (contentDisposition) {
        const fileNameMatch = contentDisposition.match(/filename="?([^"]+)"?/)
        if (fileNameMatch && fileNameMatch.length === 2)
          fileName = fileNameMatch[1]
      }
      link.setAttribute('download', fileName)
      document.body.appendChild(link)
      link.click()
      link.remove()
      // setDownloadUrl(url)
    } catch (error) {
      console.error("Error generating invoice:", error)
      alert("Failed to generate invoice")
    } finally {
      setLoading(false)
    }
  }

  return (
    <Card className="w-full max-w-4xl mx-auto my-8">
      <CardHeader>
        <CardTitle>Invoice Generator</CardTitle>
        <CardDescription>Create sales or proforma invoices.</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
          
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div className="space-y-2">
              <Label htmlFor="invoice_type">Invoice Type</Label>
              <Controller
                name="invoice_type"
                control={control}
                render={({ field }) => (
                  <Select onValueChange={field.onChange} defaultValue={field.value}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select type" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="SALES-CASH">Sales (Cash)</SelectItem>
                      <SelectItem value="SALES-LEASING">Sales (Leasing)</SelectItem>
                      <SelectItem value="PROFORMA">Proforma</SelectItem>
                    </SelectContent>
                  </Select>
                )}
              />
            </div>
            
            <div className="space-y-2">
               <Label htmlFor="model">Vehicle Model</Label>
               <Controller
                name="model"
                control={control}
                render={({ field }) => (
                  <Select onValueChange={field.onChange} defaultValue={field.value}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select Model" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="APE AUTO DX PASSENGER (Diesel)">APE AUTO DX PASSENGER (Diesel)</SelectItem>
                      <SelectItem value="APE AUTO DX PICKUP (Diesel)">APE AUTO DX PICKUP (Diesel)</SelectItem>
                    </SelectContent>
                  </Select>
                )}
              />
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 border-t pt-4">
             <div className="space-y-2">
              <Label htmlFor="dealer">Dealer Name</Label>
              <Input {...register("dealer")} />
              {errors.dealer && <p className="text-red-500 text-sm">{errors.dealer.message as string}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="dealer_contact">Dealer Contact</Label>
              <Input {...register("dealer_contact")} />
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 border-t pt-4">
             <div className="space-y-2">
              <Label htmlFor="customer">Customer Name</Label>
              <Input {...register("customer")} />
              {errors.customer && <p className="text-red-500 text-sm">{errors.customer.message as string}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="nic">Customer NIC</Label>
              <Input {...register("nic")} />
            </div>
            <div className="space-y-2 md:col-span-2">
              <Label htmlFor="cust_addr">Customer Address</Label>
              <Textarea {...register("cust_addr")} />
            </div>
          </div>

          {(isLeasing || invoiceType === "PROFORMA") && (
             <div className="grid grid-cols-1 md:grid-cols-2 gap-6 border-t pt-4 bg-slate-50 p-4 rounded-md">
                <div className="space-y-2">
                  <Label htmlFor="finance_company">Finance Company</Label>
                  <Input {...register("finance_company")} />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="finance_address">Finance Address</Label>
                  <Input {...register("finance_address")} />
                </div>
                {isLeasing && (
                  <div className="space-y-2 md:col-span-2">
                    <Label htmlFor="delivery">Delivery Address (Leasing)</Label>
                    <Textarea {...register("delivery")} />
                  </div>
                )}
             </div>
          )}

          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 border-t pt-4">
            <div className="space-y-2">
              <Label htmlFor="engine">Engine No</Label>
              <Input {...register("engine")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="chassis">Chassis No</Label>
              <Input {...register("chassis")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="color">Color</Label>
              <Input {...register("color")} />
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 border-t pt-4">
            <div className="space-y-2">
              <Label htmlFor="price">Total Price (Rs)</Label>
              <Input type="number" step="0.01" {...register("price")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="down">Down Payment (Rs)</Label>
              <Input type="number" step="0.01" {...register("down")} />
            </div>
          </div>

          <Button type="submit" className="w-full" disabled={loading}>
            {loading ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Generate PDF
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}
