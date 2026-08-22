export interface AppShellProps {
  title: string;
}

export function AppShell({ title }: AppShellProps) {
  return (
    <main className="app-shell">
      <h1>{title}</h1>
      <p>Development scaffold is ready.</p>
    </main>
  );
}
