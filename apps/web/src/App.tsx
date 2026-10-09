import { useState } from 'react'
import { InvoiceForm } from './components/InvoiceForm'
import { InvoiceList } from './components/InvoiceList'
import { Login } from './components/Login'
import { ReportsView } from './components/ReportsView'
import { StockView } from './components/StockView'
import { session, type User } from './lib/api'
import { Button } from './components/ui/button'

type View = 'new' | 'list' | 'stock' | 'reports'

const NAV: Array<{ id: View; label: string }> = [
  { id: 'new', label: 'New Invoice' },
  { id: 'list', label: 'Invoices' },
  { id: 'stock', label: 'Stock' },
  { id: 'reports', label: 'Reports' },
]

function App() {
  const [user, setUser] = useState<User | null>(() => session.getUser())
  const [view, setView] = useState<View>('new')

  if (!user) {
    return <Login onAuth={(_token, u) => setUser(u)} />
  }

  return (
    <div className="min-h-screen bg-gray-100 py-8 px-4">
      <div className="max-w-5xl mx-auto mb-6 flex flex-wrap items-center justify-between gap-4">
        <div className="text-center md:text-left">
          <h1 className="text-3xl font-bold text-slate-900">Invoice App</h1>
          <p className="text-slate-600 text-sm">Gunawardhana Motors — dealer management</p>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          {NAV.map((n) => (
            <Button
              key={n.id}
              variant={view === n.id ? 'default' : 'outline'}
              size="sm"
              onClick={() => setView(n.id)}
            >
              {n.label}
            </Button>
          ))}
          <span className="text-xs text-slate-500 ml-2">
            {user.name} · {user.role}
          </span>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              session.clear()
              setUser(null)
            }}
          >
            Sign out
          </Button>
        </div>
      </div>
      {view === 'new' && <InvoiceForm />}
      {view === 'list' && <InvoiceList />}
      {view === 'stock' && <StockView />}
      {view === 'reports' && <ReportsView />}
    </div>
  )
}

export default App
