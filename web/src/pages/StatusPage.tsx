import React from 'react'
import { Server, Cpu, HardDrive, Activity, Users } from 'lucide-react'

export const StatusPage: React.FC = () => {
  return (
    <div className="p-6">
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-foreground mb-2">Status</h1>
        <p className="text-muted-foreground">
          Monitor system status and node information
        </p>
      </div>

      {/* Status cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
        <div className="bg-card border border-border rounded-lg p-6">
          <div className="flex items-center gap-3 mb-2">
            <div className="w-10 h-10 bg-blue-500/10 rounded-lg flex items-center justify-center">
              <Server className="w-5 h-5 text-blue-500" />
            </div>
            <div>
              <h3 className="font-semibold text-foreground">Nodes</h3>
              <p className="text-2xl font-bold text-foreground">3</p>
            </div>
          </div>
          <p className="text-sm text-muted-foreground">Active nodes</p>
        </div>

        <div className="bg-card border border-border rounded-lg p-6">
          <div className="flex items-center gap-3 mb-2">
            <div className="w-10 h-10 bg-green-500/10 rounded-lg flex items-center justify-center">
              <Activity className="w-5 h-5 text-green-500" />
            </div>
            <div>
              <h3 className="font-semibold text-foreground">Applications</h3>
              <p className="text-2xl font-bold text-foreground">12</p>
            </div>
          </div>
          <p className="text-sm text-muted-foreground">Running instances</p>
        </div>

        <div className="bg-card border border-border rounded-lg p-6">
          <div className="flex items-center gap-3 mb-2">
            <div className="w-10 h-10 bg-purple-500/10 rounded-lg flex items-center justify-center">
              <Cpu className="w-5 h-5 text-purple-500" />
            </div>
            <div>
              <h3 className="font-semibold text-foreground">CPU Usage</h3>
              <p className="text-2xl font-bold text-foreground">45%</p>
            </div>
          </div>
          <p className="text-sm text-muted-foreground">Average across nodes</p>
        </div>

        <div className="bg-card border border-border rounded-lg p-6">
          <div className="flex items-center gap-3 mb-2">
            <div className="w-10 h-10 bg-orange-500/10 rounded-lg flex items-center justify-center">
              <HardDrive className="w-5 h-5 text-orange-500" />
            </div>
            <div>
              <h3 className="font-semibold text-foreground">Storage</h3>
              <p className="text-2xl font-bold text-foreground">2.1TB</p>
            </div>
          </div>
          <p className="text-sm text-muted-foreground">Available space</p>
        </div>
      </div>

      {/* Node information */}
      <div className="bg-card border border-border rounded-lg p-6">
        <h2 className="text-xl font-semibold text-foreground mb-4">Node Information</h2>
        
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b border-border">
                <th className="text-left py-3 px-4 font-medium text-muted-foreground">Node</th>
                <th className="text-left py-3 px-4 font-medium text-muted-foreground">Status</th>
                <th className="text-left py-3 px-4 font-medium text-muted-foreground">CPU</th>
                <th className="text-left py-3 px-4 font-medium text-muted-foreground">Memory</th>
                <th className="text-left py-3 px-4 font-medium text-muted-foreground">Applications</th>
              </tr>
            </thead>
            <tbody>
              {[
                { name: 'node-1', status: 'Active', cpu: '32%', memory: '4.2GB / 16GB', apps: 4 },
                { name: 'node-2', status: 'Active', cpu: '58%', memory: '8.1GB / 32GB', apps: 6 },
                { name: 'node-3', status: 'Active', cpu: '45%', memory: '2.8GB / 8GB', apps: 2 },
              ].map((node, index) => (
                <tr key={index} className="border-b border-border hover:bg-accent/50">
                  <td className="py-3 px-4 font-medium text-foreground">{node.name}</td>
                  <td className="py-3 px-4">
                    <span className="inline-flex items-center px-2 py-1 rounded-full text-xs bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300">
                      {node.status}
                    </span>
                  </td>
                  <td className="py-3 px-4 text-muted-foreground">{node.cpu}</td>
                  <td className="py-3 px-4 text-muted-foreground">{node.memory}</td>
                  <td className="py-3 px-4 text-muted-foreground">{node.apps}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
} 