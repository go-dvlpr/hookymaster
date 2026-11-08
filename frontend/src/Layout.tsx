import { useCallback, useEffect, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useOutletContext } from 'react-router-dom'
import { api, type Nav } from './api'

interface LayoutContext { refreshNav: () => void }

export const useLayout = () => useOutletContext<LayoutContext>()

const sideClass = ({ isActive }: { isActive: boolean }) => 'side' + (isActive ? ' active' : '')

export function Layout() {
  const [nav, setNav] = useState<Nav | null>(null)
  const location = useLocation()

  const refreshNav = useCallback(() => {
    api.nav().then(setNav).catch(() => setNav(null))
  }, [])

  useEffect(refreshNav, [refreshNav, location.pathname])

  const projectCode = location.pathname.match(/^\/projects\/(O\d+)/)?.[1]

  return (
    <div className="shell">
      <aside className="sidebar" aria-label="Sidebar">
        <Link to="/" className="logo">
          <svg width="28" height="28" viewBox="0 0 28 28" aria-hidden="true">
            <defs>
              <linearGradient id="lg" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stopColor="#7C3AED" />
                <stop offset="1" stopColor="#D946EF" />
              </linearGradient>
            </defs>
            <rect x="1" y="1" width="26" height="26" rx="8" fill="none" stroke="url(#lg)" strokeWidth="2" />
            <path d="M10 8v6a4 4 0 0 0 8 0" fill="none" stroke="url(#lg)" strokeWidth="2.4" strokeLinecap="round" />
            <circle cx="18" cy="9" r="1.6" fill="#D946EF" />
          </svg>
          HOOKYMASTER
        </Link>
        <nav className="side-group" aria-label="Main">
          <NavLink to="/" end className={sideClass}>Overview</NavLink>
          <NavLink to="/queue" className={sideClass}>
            Queue{nav && nav.readyToExport > 0 && <span className="cnt">{nav.readyToExport}</span>}
          </NavLink>
        </nav>
        <div className="side-group">
          <span className="side-title">PROJECTS</span>
          {nav?.projects.map((p) => (
            <Link key={p.code} to={`/projects/${p.code}`} className={'side' + (projectCode === p.code ? ' active' : '')}>
              <span className="code">{p.code}</span>
              {p.name}
              {p.openCandidates > 0 && <span className="cnt violet">{p.openCandidates}</span>}
            </Link>
          ))}
          <NavLink to="/projects/new" className={sideClass} style={{ color: '#c4b5fd' }}>+ New project</NavLink>
        </div>
      </aside>
      <main className="main">
        <Outlet context={{ refreshNav } satisfies LayoutContext} />
      </main>
    </div>
  )
}
