import { useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { Button } from '../components/ui/Button'
import { Input } from '../components/ui/Input'
import { ErrorBanner } from '../components/ErrorBanner'
import { ApiError } from '../api'

export function LoginPage() {
  const { login, register } = useAuth()
  const navigate = useNavigate()
  const location = useLocation() as { state?: { from?: string } }
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [submitting, setSubmitting] = useState(false)

  const fieldError = (field: string) => (error instanceof ApiError ? error.fieldError(field) : undefined)
  const target = location.state?.from ?? '/'

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      if (mode === 'login') await login(email, password)
      else await register(email, password, displayName)
      navigate(target, { replace: true })
    } catch (err) {
      setError(err)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="mx-auto flex min-h-screen max-w-md flex-col justify-center px-4">
      <h1 className="mb-6 text-center text-2xl font-semibold text-brand-700">JobSeeker</h1>
      <form onSubmit={onSubmit} className="space-y-4 rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
        <h2 className="text-lg font-semibold">{mode === 'login' ? 'Sign in' : 'Create account'}</h2>
        <ErrorBanner error={error} />
        {mode === 'register' && (
          <Input
            label="Display name"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            error={fieldError('display_name')}
            autoComplete="name"
            required
          />
        )}
        <Input
          label="Email"
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          error={fieldError('email')}
          autoComplete="email"
          required
        />
        <Input
          label="Password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={fieldError('password')}
          autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
          required
        />
        <Button type="submit" disabled={submitting} className="w-full">
          {submitting ? 'Please wait…' : mode === 'login' ? 'Sign in' : 'Create account'}
        </Button>
        <button
          type="button"
          onClick={() => {
            setMode(mode === 'login' ? 'register' : 'login')
            setError(null)
          }}
          className="w-full text-center text-sm text-brand-600 hover:underline"
        >
          {mode === 'login' ? 'Need an account? Register' : 'Already have an account? Sign in'}
        </button>
      </form>
    </div>
  )
}
