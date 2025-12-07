import { InvoiceForm } from './components/InvoiceForm'

function App() {
  return (
    <div className="min-h-screen bg-gray-100 py-8 px-4">
      <div className="max-w-5xl mx-auto mb-8 text-center">
        <h1 className="text-4xl font-bold text-slate-900 mb-2">Invoice App</h1>
        <p className="text-slate-600">Create and manage invoices efficiently</p>
      </div>
      <InvoiceForm />
    </div>
  )
}

export default App
