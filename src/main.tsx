import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route, Navigate, Outlet } from 'react-router-dom'
import { ThemeProvider } from 'next-themes'

// CSS
import '@/app/globals.css'

// Pages
import LoginPage from '@/app/login/page'
import DashboardPage from '@/app/(admin)/page'
import AdminLayoutInner from '@/app/(admin)/layout'
import DbViewerPage from '@/app/(admin)/db/[name]/page'
import DbSettingsPage from '@/app/(admin)/db/[name]/settings/page'
import DbFilesPage from '@/app/(admin)/db/[name]/files/page'
import DbNewPage from '@/app/(admin)/db/new/page'
import DeletedPage from '@/app/(admin)/deleted/page'
import SystemDbViewerPage from '@/app/(admin)/db/_system/[name]/page'
import AuthGuard from '@/components/auth-guard'

/**
 * Wraps the Next.js-style AdminLayout (children prop) with React Router's <Outlet>.
 * Outlet provides the matched child route's element as children.
 */
function AdminLayoutRoute() {
  return (
    <AdminLayoutInner>
      <Outlet />
    </AdminLayoutInner>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ThemeProvider attribute="class" defaultTheme="dark" disableTransitionOnChange>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />

          {/* All admin routes require authentication */}
          <Route element={<AuthGuard />}>
            <Route element={<AdminLayoutRoute />}>
              <Route path="/" element={<DashboardPage />} />
              <Route path="/db/new" element={<DbNewPage />} />
              <Route path="/db/_system/:name" element={<SystemDbViewerPage />} />
              <Route path="/db/:name" element={<DbViewerPage />} />
              <Route path="/db/:name/settings" element={<DbSettingsPage />} />
              <Route path="/db/:name/files" element={<DbFilesPage />} />
              <Route path="/deleted" element={<DeletedPage />} />
            </Route>
          </Route>

          {/* Fallback */}
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </ThemeProvider>
  </React.StrictMode>
)
