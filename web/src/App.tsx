import { Navigate, Route, Routes } from 'react-router-dom'
import { ErrorBoundary } from './components/ErrorBoundary'
import { Layout } from './components/Layout'
import { ProtectedRoute } from './components/ProtectedRoute'
import { LoginPage } from './pages/LoginPage'
import { DashboardPage } from './pages/DashboardPage'
import { JobDiscoveryPage } from './pages/JobDiscoveryPage'
import { ProfilePage } from './pages/ProfilePage'
import { SearchConfigPage } from './pages/SearchConfigPage'
import { SearchHistoryPage } from './pages/SearchHistoryPage'
import { SavedJobsPage } from './pages/SavedJobsPage'

export function AppRoutes() {
  return (
    <ErrorBoundary>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          element={
            <ProtectedRoute>
              <Layout />
            </ProtectedRoute>
          }
        >
          <Route path="/" element={<DashboardPage />} />
          <Route path="/jobs" element={<JobDiscoveryPage />} />
          <Route path="/profile" element={<ProfilePage />} />
          <Route path="/search-config" element={<SearchConfigPage />} />
          <Route path="/history" element={<SearchHistoryPage />} />
          <Route path="/saved" element={<SavedJobsPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </ErrorBoundary>
  )
}
