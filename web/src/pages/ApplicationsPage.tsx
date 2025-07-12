import React from 'react'
import { Box, Plus, Search, Filter } from 'lucide-react'

export const ApplicationsPage: React.FC = () => {
  return (
    <div className="p-6">
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-foreground mb-2">Applications</h1>
        <p className="text-muted-foreground">
          Manage and monitor your application instances
        </p>
      </div>

      {/* Action bar */}
      <div className="flex flex-col sm:flex-row gap-4 mb-6">
        <div className="flex-1 relative">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground" size={20} />
          <input
            type="text"
            placeholder="Search applications..."
            className="w-full pl-10 pr-4 py-2 border border-input rounded-lg bg-background text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
          />
        </div>
        <div className="flex gap-2">
          <button className="flex items-center gap-2 px-4 py-2 border border-input rounded-lg bg-background text-foreground hover:bg-accent">
            <Filter size={16} />
            Filter
          </button>
          <button className="flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90">
            <Plus size={16} />
            New Application
          </button>
        </div>
      </div>

      {/* Applications grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {/* Placeholder cards */}
        {[1, 2, 3, 4, 5, 6].map((i) => (
          <div key={i} className="bg-card border border-border rounded-lg p-6 hover:shadow-lg transition-shadow">
            <div className="flex items-center gap-3 mb-4">
              <div className="w-10 h-10 bg-primary/10 rounded-lg flex items-center justify-center">
                <Box className="w-5 h-5 text-primary" />
              </div>
              <div>
                <h3 className="font-semibold text-foreground">Application {i}</h3>
                <p className="text-sm text-muted-foreground">ubuntu-20.04</p>
              </div>
            </div>
            
            <div className="space-y-2 mb-4">
              <div className="flex justify-between text-sm">
                <span className="text-muted-foreground">Status</span>
                <span className="text-green-600 font-medium">Running</span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-muted-foreground">Created</span>
                <span className="text-foreground">2 hours ago</span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-muted-foreground">Node</span>
                <span className="text-foreground">node-{i}</span>
              </div>
            </div>
            
            <div className="flex gap-2">
              <button className="flex-1 px-3 py-1 text-sm bg-secondary text-secondary-foreground rounded hover:bg-secondary/80">
                View
              </button>
              <button className="flex-1 px-3 py-1 text-sm border border-input rounded hover:bg-accent">
                Manage
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
} 