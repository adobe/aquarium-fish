import React from 'react'
import { Tag, Users, Plus, Settings } from 'lucide-react'

export const ManagePage: React.FC = () => {
  return (
    <div className="p-6">
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-foreground mb-2">Manage</h1>
        <p className="text-muted-foreground">
          Manage labels, users, and system configuration
        </p>
      </div>

      {/* Management sections */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Labels section */}
        <div className="bg-card border border-border rounded-lg p-6">
          <div className="flex items-center justify-between mb-4">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 bg-blue-500/10 rounded-lg flex items-center justify-center">
                <Tag className="w-5 h-5 text-blue-500" />
              </div>
              <div>
                <h2 className="text-xl font-semibold text-foreground">Labels</h2>
                <p className="text-sm text-muted-foreground">Manage resource labels</p>
              </div>
            </div>
            <button className="flex items-center gap-2 px-3 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90">
              <Plus size={16} />
              Add Label
            </button>
          </div>
          
          <div className="space-y-3">
            {[
              { name: 'ubuntu-20.04', description: 'Ubuntu 20.04 LTS', count: 5 },
              { name: 'windows-server-2019', description: 'Windows Server 2019', count: 3 },
              { name: 'macos-monterey', description: 'macOS Monterey', count: 2 },
            ].map((label, index) => (
              <div key={index} className="flex items-center justify-between p-3 border border-border rounded-lg">
                <div>
                  <h3 className="font-medium text-foreground">{label.name}</h3>
                  <p className="text-sm text-muted-foreground">{label.description}</p>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-sm text-muted-foreground">{label.count} instances</span>
                  <button className="p-1 text-muted-foreground hover:text-foreground">
                    <Settings size={16} />
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Users section */}
        <div className="bg-card border border-border rounded-lg p-6">
          <div className="flex items-center justify-between mb-4">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 bg-green-500/10 rounded-lg flex items-center justify-center">
                <Users className="w-5 h-5 text-green-500" />
              </div>
              <div>
                <h2 className="text-xl font-semibold text-foreground">Users</h2>
                <p className="text-sm text-muted-foreground">Manage user accounts</p>
              </div>
            </div>
            <button className="flex items-center gap-2 px-3 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90">
              <Plus size={16} />
              Add User
            </button>
          </div>
          
          <div className="space-y-3">
            {[
              { name: 'admin', roles: ['Admin'], active: true },
              { name: 'developer', roles: ['User', 'Power'], active: true },
              { name: 'viewer', roles: ['User'], active: false },
            ].map((user, index) => (
              <div key={index} className="flex items-center justify-between p-3 border border-border rounded-lg">
                <div>
                  <h3 className="font-medium text-foreground">{user.name}</h3>
                  <p className="text-sm text-muted-foreground">{user.roles.join(', ')}</p>
                </div>
                <div className="flex items-center gap-2">
                  <span className={`text-sm px-2 py-1 rounded-full ${
                    user.active 
                      ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300' 
                      : 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-300'
                  }`}>
                    {user.active ? 'Active' : 'Inactive'}
                  </span>
                  <button className="p-1 text-muted-foreground hover:text-foreground">
                    <Settings size={16} />
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
} 