import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'ANTIGRAVITY NOC // Real-Time Cockpit',
  description: 'High-performance real-time network operations center dashboard',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <body className="antialiased selection:bg-slate-800 selection:text-cyan-400">
        {children}
      </body>
    </html>
  )
}
