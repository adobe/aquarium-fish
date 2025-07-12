import React, { useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'
import { useTheme } from '../contexts/ThemeContext'
import { 
  Menu, 
  X, 
  LogOut, 
  User, 
  Server, 
  Settings, 
  Monitor, 
  Sun, 
  Moon,
  Box
} from 'lucide-react'

interface LayoutProps {
  children: React.ReactNode
}

export const Layout: React.FC<LayoutProps> = ({ children }) => {
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const { user, logout } = useAuth()
  const { theme, setTheme, effectiveTheme } = useTheme()
  const location = useLocation()

  const navigation = [
    { name: 'Applications', href: '/applications', icon: Box },
    { name: 'Status', href: '/status', icon: Server },
    { name: 'Manage', href: '/manage', icon: Settings },
  ]

  const handleLogout = () => {
    logout()
  }

  const toggleTheme = () => {
    const themes = ['light', 'dark', 'system'] as const
    const currentIndex = themes.indexOf(theme)
    const nextIndex = (currentIndex + 1) % themes.length
    setTheme(themes[nextIndex])
  }

  return (
    <div className="min-h-screen bg-background">
      {/* Mobile sidebar backdrop */}
      {sidebarOpen && (
        <div 
          className="fixed inset-0 bg-black/50 lg:hidden z-40"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Sidebar */}
      <div className={`fixed inset-y-0 left-0 z-50 w-64 bg-card border-r border-border transform transition-transform duration-300 lg:translate-x-0 ${
        sidebarOpen ? 'translate-x-0' : '-translate-x-full'
      } lg:static lg:inset-0 lg:flex lg:w-64 lg:flex-col`}>
        <div className="flex flex-col h-full">
          {/* Header */}
          <div className="flex items-center justify-between p-4 border-b border-border">
            <div className="flex items-center gap-2">
              <div className="w-8 h-8 bg-primary rounded-lg flex items-center justify-center">
                <Box className="w-5 h-5 text-primary-foreground" />
              </div>
              <h1 className="text-lg font-semibold text-foreground">Aquarium Fish</h1>
            </div>
            <button
              onClick={() => setSidebarOpen(false)}
              className="lg:hidden text-muted-foreground hover:text-foreground"
            >
              <X size={20} />
            </button>
          </div>

          {/* Navigation */}
          <nav className="flex-1 px-4 py-4">
            <ul className="space-y-2">
              {navigation.map((item) => {
                const isActive = location.pathname === item.href || 
                  (item.href === '/applications' && location.pathname === '/')
                const Icon = item.icon
                
                return (
                  <li key={item.name}>
                    <Link
                      to={item.href}
                      onClick={() => setSidebarOpen(false)}
                      className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors ${
                        isActive
                          ? 'bg-primary text-primary-foreground'
                          : 'text-muted-foreground hover:text-foreground hover:bg-accent'
                      }`}
                    >
                      <Icon size={18} />
                      {item.name}
                    </Link>
                  </li>
                )
              })}
            </ul>
          </nav>

          {/* User menu */}
          <div className="border-t border-border p-4">
            <div className="flex items-center gap-3 mb-4">
              <div className="w-8 h-8 bg-secondary rounded-full flex items-center justify-center">
                <User size={16} className="text-secondary-foreground" />
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium text-foreground truncate">
                  {user?.name || 'User'}
                </p>
                <p className="text-xs text-muted-foreground truncate">
                  {user?.roles?.join(', ') || 'No roles'}
                </p>
              </div>
            </div>

            <div className="flex items-center gap-2">
              <button
                onClick={toggleTheme}
                className="flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:text-foreground rounded-lg hover:bg-accent flex-1"
                title="Toggle theme"
              >
                {effectiveTheme === 'dark' ? (
                  <Moon size={16} />
                ) : (
                  <Sun size={16} />
                )}
                <span className="capitalize">{theme}</span>
              </button>
              
              <button
                onClick={handleLogout}
                className="flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:text-foreground rounded-lg hover:bg-accent"
                title="Sign out"
              >
                <LogOut size={16} />
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Main content */}
      <div className="lg:ml-64 flex flex-col min-h-screen">
        {/* Top bar */}
        <header className="bg-card border-b border-border px-4 py-3 lg:px-6">
          <div className="flex items-center">
            <button
              onClick={() => setSidebarOpen(true)}
              className="lg:hidden text-muted-foreground hover:text-foreground mr-4"
            >
              <Menu size={20} />
            </button>
            
            <div className="flex items-center gap-4">
              <h2 className="text-xl font-semibold text-foreground">
                {navigation.find(nav => nav.href === location.pathname)?.name || 'Dashboard'}
              </h2>
            </div>
          </div>
        </header>

        {/* Page content */}
        <main className="flex-1 overflow-auto">
          {children}
        </main>
      </div>
    </div>
  )
} 