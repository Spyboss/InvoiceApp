import { useState } from 'react'
import { InvoiceForm } from './components/InvoiceForm'
import { InvoiceList } from './components/InvoiceList'
import { Login } from './components/Login'
import { session, type User } from './lib/api'
import { Button } from './components/ui/button'

type View = 'new' | 'list'

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
        <div className="flex items-center gap-2">
          <Button variant={view === 'new' ? 'default' : 'outline'} size="sm" onClick={() => setView('new')}>
            New Invoice
          </Button>
          <Button variant={view === 'list' ? 'default' : 'outline'} size="sm" onClick={() => setView('list')}>
            Invoices
          </Button>
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
      {view === 'new' ? <InvoiceForm /> : <InvoiceList />}
    </div>
  )
}

export default App
