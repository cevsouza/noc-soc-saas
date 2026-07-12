import type { Metadata } from 'next'
import './globals.css'
import { ThemeProvider } from '@/lib/theme-provider'
import { AuthProvider } from '@/lib/auth-context'

export const metadata: Metadata = {
  title: 'ITFácil NOC // Real-Time Cockpit',
  description: 'High-performance real-time network operations center dashboard',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="antialiased selection:bg-slate-800 selection:text-cyan-400">
        <ThemeProvider attribute="class" defaultTheme="dark" enableSystem>
          <AuthProvider>{children}</AuthProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
