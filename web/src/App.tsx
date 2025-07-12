import React from 'react'
import { BrowserRouter as Router, Routes, Route } from 'react-router-dom'
import { AuthProvider } from './contexts/AuthContext'
import { ThemeProvider } from './contexts/ThemeContext'
import { Layout } from './components/Layout'
import { LoginPage } from './pages/LoginPage'
import { ApplicationsPage } from './pages/ApplicationsPage'
import { StatusPage } from './pages/StatusPage'
import { ManagePage } from './pages/ManagePage'
import { ProtectedRoute } from './components/ProtectedRoute'

function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <Router>
          <div className="min-h-screen bg-background">
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route
                path="/*"
                element={
                  <ProtectedRoute>
                    <Layout>
                      <Routes>
                        <Route path="/" element={<ApplicationsPage />} />
                        <Route path="/applications" element={<ApplicationsPage />} />
                        <Route path="/status" element={<StatusPage />} />
                        <Route path="/manage" element={<ManagePage />} />
                        <Route path="*" element={<ApplicationsPage />} />
                      </Routes>
                    </Layout>
                  </ProtectedRoute>
                }
              />
            </Routes>
          </div>
        </Router>
      </AuthProvider>
    </ThemeProvider>
  )
}

export default App 