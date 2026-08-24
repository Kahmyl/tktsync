import { useEffect, useState, type ComponentType } from 'react';
import {
  Building2,
  CalendarDays,
  ChartNoAxesColumn,
  LayoutDashboard,
  LogOut,
  Menu,
  PanelLeftClose,
  PanelLeftOpen,
  Plug,
  ScanLine,
  Settings,
  Ticket,
  Users,
  X,
} from 'lucide-react';
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useOperator } from '../auth/OperatorSession';
import { Button, Logo } from '../components/ui';
import { initials } from '../lib/format';

interface NavItem {
  label: string;
  to: string;
  icon: ComponentType<{ size?: number }>;
}

const navGroups: Array<{ title: string; items: NavItem[] }> = [
  { title: 'Overview', items: [{ label: 'Dashboard', to: '/', icon: LayoutDashboard }] },
  {
    title: 'Operations',
    items: [
      { label: 'Events', to: '/events', icon: CalendarDays },
      { label: 'Venues', to: '/venues', icon: Building2 },
      { label: 'Tickets', to: '/tickets', icon: Ticket },
      { label: 'Admissions', to: '/admissions', icon: ScanLine },
    ],
  },
  {
    title: 'Partners',
    items: [
      { label: 'Partners', to: '/partners', icon: Users },
      { label: 'Integrations', to: '/integrations', icon: Plug },
    ],
  },
  { title: 'Insights', items: [{ label: 'Reports', to: '/reports', icon: ChartNoAxesColumn }] },
];

function routeMeta(pathname: string) {
  if (pathname === '/') return { title: 'Dashboard' };
  if (pathname === '/events/new')
    return { title: 'Create event', parent: ['Events', '/events'] as const };
  if (/^\/events\//.test(pathname))
    return { title: 'Event', parent: ['Events', '/events'] as const };
  if (pathname === '/events') return { title: 'Events', group: 'Operations' };
  if (/^\/venues\//.test(pathname))
    return { title: 'Venue', parent: ['Venues', '/venues'] as const };
  if (pathname === '/venues') return { title: 'Venues', group: 'Operations' };
  if (/^\/partners\//.test(pathname))
    return { title: 'Partner', parent: ['Partners', '/partners'] as const };
  if (pathname === '/partners') return { title: 'Partners', group: 'Partners' };
  if (pathname === '/tickets') return { title: 'Tickets', group: 'Operations' };
  if (pathname === '/admissions') return { title: 'Admissions', group: 'Operations' };
  if (pathname === '/integrations') return { title: 'Integrations', group: 'Partners' };
  if (pathname === '/reports') return { title: 'Reports', group: 'Insights' };
  return { title: 'Account', group: 'Account' };
}

function NavList({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
  return (
    <nav className="side-nav" aria-label="Admin navigation">
      {navGroups.map((group) => (
        <div className="nav-group" key={group.title}>
          {collapsed ? <div className="nav-separator" /> : <p>{group.title}</p>}
          <ul>
            {group.items.map((item) => (
              <li key={item.to}>
                <NavLink
                  to={item.to}
                  end={item.to === '/'}
                  onClick={onNavigate}
                  title={collapsed ? item.label : undefined}
                  className={({ isActive }) => (isActive ? 'active' : '')}
                >
                  <item.icon size={16} />
                  {collapsed ? null : <span>{item.label}</span>}
                </NavLink>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  );
}

function SignOutNav({ collapsed }: { collapsed: boolean }) {
  const { signOut } = useOperator();
  const navigate = useNavigate();
  const logout = async () => {
    await signOut();
    navigate('/sign-in', { replace: true });
  };
  return (
    <div className={`sidebar-signout ${collapsed ? 'collapsed' : ''}`}>
      <button
        type="button"
        aria-label="Sign Out"
        title={collapsed ? 'Sign Out' : undefined}
        onClick={() => void logout()}
      >
        <LogOut size={16} />
        {collapsed ? null : <span>Sign Out</span>}
      </button>
    </div>
  );
}

function AccountMenu() {
  const { user, signOut } = useOperator();
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  if (!user) return null;
  const logout = async () => {
    setOpen(false);
    await signOut();
    navigate('/sign-in', { replace: true });
  };
  return (
    <div className="account-menu">
      <button
        type="button"
        aria-label="Account menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="avatar avatar-button"
      >
        {initials(user.displayName)}
      </button>
      {open ? (
        <div className="account-popover">
          <div className="account-summary">
            <span className="avatar">{initials(user.displayName)}</span>
            <div>
              <strong>{user.displayName}</strong>
              <small>{user.email}</small>
            </div>
          </div>
          <Link to="/account" onClick={() => setOpen(false)}>
            <Settings size={16} />
            Account settings
          </Link>
          <button type="button" onClick={() => void logout()}>
            <LogOut size={16} />
            Log out
          </button>
        </div>
      ) : null}
    </div>
  );
}

export function AdminLayout() {
  const [collapsed, setCollapsed] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const location = useLocation();
  const meta = routeMeta(location.pathname);

  useEffect(() => setDrawerOpen(false), [location.pathname]);

  return (
    <div className="admin-shell">
      <aside className={`sidebar ${collapsed ? 'collapsed' : ''}`}>
        <div className="sidebar-logo">
          <Link to="/">
            <Logo showWordmark={!collapsed} size={collapsed ? 26 : 28} />
          </Link>
        </div>
        <div className="sidebar-scroll">
          <NavList collapsed={collapsed} />
        </div>
        <div className="sidebar-collapse">
          <Button
            variant="ghost"
            size={collapsed ? 'icon' : 'small'}
            aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            onClick={() => setCollapsed((value) => !value)}
          >
            {collapsed ? <PanelLeftOpen size={16} /> : <PanelLeftClose size={16} />}
            {collapsed ? null : 'Collapse'}
          </Button>
        </div>
        <SignOutNav collapsed={collapsed} />
      </aside>

      {drawerOpen ? (
        <div className="drawer-backdrop" onMouseDown={() => setDrawerOpen(false)} />
      ) : null}
      <aside className={`mobile-drawer ${drawerOpen ? 'open' : ''}`} aria-hidden={!drawerOpen}>
        <div className="sidebar-logo">
          <Logo />
          <button type="button" aria-label="Close navigation" onClick={() => setDrawerOpen(false)}>
            <X size={20} />
          </button>
        </div>
        <div className="sidebar-scroll">
          <NavList collapsed={false} onNavigate={() => setDrawerOpen(false)} />
        </div>
        <SignOutNav collapsed={false} />
      </aside>

      <div className="admin-column">
        <header className="topbar">
          <button
            type="button"
            className="mobile-menu"
            aria-label="Open navigation"
            onClick={() => setDrawerOpen(true)}
          >
            <Menu size={20} />
          </button>
          <div className="topbar-title">
            {'parent' in meta && meta.parent ? (
              <span>
                <Link to={meta.parent[1]}>{meta.parent[0]}</Link>
                <i>/</i>
              </span>
            ) : 'group' in meta && meta.group ? (
              <span>
                {meta.group}
                <i>/</i>
              </span>
            ) : null}
            <strong>{meta.title}</strong>
          </div>
          <AccountMenu />
        </header>
        <main className="admin-main">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
