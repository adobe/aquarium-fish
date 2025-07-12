import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react'

type Theme = 'light' | 'dark' | 'system'

interface ThemeContextType {
  theme: Theme
  setTheme: (theme: Theme) => void
  effectiveTheme: 'light' | 'dark'
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined)

export const useTheme = () => {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return context
}

interface ThemeProviderProps {
  children: ReactNode
}

export const ThemeProvider: React.FC<ThemeProviderProps> = ({ children }) => {
  const [theme, setTheme] = useState<Theme>('system')
  const [effectiveTheme, setEffectiveTheme] = useState<'light' | 'dark'>('light')

  useEffect(() => {
    // Load theme from localStorage or default to system
    const savedTheme = localStorage.getItem('theme') as Theme
    if (savedTheme && ['light', 'dark', 'system'].includes(savedTheme)) {
      setTheme(savedTheme)
    }
  }, [])

  useEffect(() => {
    const updateEffectiveTheme = () => {
      let newEffectiveTheme: 'light' | 'dark'
      
      if (theme === 'system') {
        newEffectiveTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
      } else {
        newEffectiveTheme = theme
      }
      
      setEffectiveTheme(newEffectiveTheme)
      
      // Apply theme to document
      if (newEffectiveTheme === 'dark') {
        document.documentElement.classList.add('dark')
      } else {
        document.documentElement.classList.remove('dark')
      }
    }

    updateEffectiveTheme()

    // Listen for system theme changes
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    const handleSystemThemeChange = () => {
      if (theme === 'system') {
        updateEffectiveTheme()
      }
    }

    mediaQuery.addEventListener('change', handleSystemThemeChange)
    
    return () => {
      mediaQuery.removeEventListener('change', handleSystemThemeChange)
    }
  }, [theme])

  const handleSetTheme = (newTheme: Theme) => {
    setTheme(newTheme)
    localStorage.setItem('theme', newTheme)
  }

  const value: ThemeContextType = {
    theme,
    setTheme: handleSetTheme,
    effectiveTheme,
  }

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
} 