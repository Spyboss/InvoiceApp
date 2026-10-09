import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Loader2 } from 'lucide-react'
import { api, ApiError, session, type User } from '../lib/api'

interface LoginValues {
  name?: string
  email: string
  password: string
}

export function Login({ onAuth }: { onAuth: (token: string, user: User) => void }) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const { register, handleSubmit, formState: { errors } } = useForm<LoginValues>({
    defaultValues: { email: '', password: '' },
  })

  const submit = async (values: LoginValues) => {
    setLoading(true)
    setError(null)
    try {
      const result =
        mode === 'login'
          ? await api.login(values.email, values.password)
          : await api.register({ name: values.name ?? '', email: values.email, password: values.password })
      session.set(result.token, result.user)
      onAuth(result.token, result.user)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Connection failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-100 flex items-center justify-center px-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle className="text-2xl">{mode === 'login' ? 'Sign in' : 'Create account'}</CardTitle>
          <CardDescription>
            {mode === 'login'
              ? 'Gunawardhana Motors — dealer management'
              : 'The first account becomes the admin.'}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(submit)} className="space-y-4">
            {mode === 'register' && (
              <div className="space-y-2">
                <Label htmlFor="name">Name</Label>
                <Input id="name" {...register('name', { required: 'Name is required' })} />
                {errors.name && <p className="text-red-500 text-sm">{errors.name.message}</p>}
              </div>
            )}
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input id="email" type="email" autoComplete="email" {...register('email', { required: 'Email is required' })} />
              {errors.email && <p className="text-red-500 text-sm">{errors.email.message}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                {...register('password', {
                  required: 'Password is required',
                  minLength: mode === 'register' ? { value: 8, message: 'At least 8 characters' } : undefined,
                })}
              />
              {errors.password && <p className="text-red-500 text-sm">{errors.password.message}</p>}
            </div>
            {error && <p className="text-red-500 text-sm">{error}</p>}
            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              {mode === 'login' ? 'Sign in' : 'Register'}
            </Button>
            <button
              type="button"
              className="w-full text-sm text-slate-500 hover:text-slate-800"
              onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setError(null) }}
            >
              {mode === 'login' ? 'No account yet? Register the first user' : 'Already registered? Sign in'}
            </button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
