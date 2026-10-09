import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { Button } from './ui/Button'

const NAV = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/jobs', label: 'Job Discovery' },
  { to: '/profile', label: 'Profile' },
  { to: '/search-config', label: 'Search Config' },
  { to: '/history', label: 'History' },
  { to: '/saved', label: 'Saved & Applications' },
]

export function Layout() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()

  const onLogout = async () => {
    await logout()
    navigate('/login')
  }

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3 px-4 py-3">
          <NavLink to="/" className="text-lg font-semibold text-brand-700">
            JobSeeker
          </NavLink>
          <div className="flex items-center gap-3">
            {user && <span className="text-sm text-slate-600">{user.display_name}</span>}
            <Button variant="secondary" onClick={onLogout}>
              Sign out
            </Button>
          </div>
        </div>
        <nav aria-label="Primary" className="mx-auto max-w-6xl px-4">
          <ul className="flex flex-wrap gap-1 pb-1">
            {NAV.map((item) => (
              <li key={item.to}>
                <NavLink
                  to={item.to}
                  end={item.end}
                  className={({ isActive }) =>
                    `inline-block rounded-md px-3 py-2 text-sm font-medium ${
                      isActive ? 'bg-brand-50 text-brand-700' : 'text-slate-600 hover:bg-slate-100'
                    }`
                  }
                >
                  {item.label}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}
