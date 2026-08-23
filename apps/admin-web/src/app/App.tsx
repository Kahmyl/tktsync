import { Navigate, Route, Routes } from 'react-router-dom';
import { useOperator } from '../auth/OperatorSession';
import { LoadingState } from '../components/ui';
import { AccountPage } from '../features/account/AccountPage';
import { AdmissionsPage } from '../features/admissions/AdmissionsPage';
import { SignInPage } from '../features/auth/SignInPage';
import { DashboardPage } from '../features/dashboard/DashboardPage';
import { CreateEventPage } from '../features/events/CreateEventPage';
import { EventDetailPage } from '../features/events/EventDetailPage';
import { EventsListPage } from '../features/events/EventsListPage';
import { IntegrationsPage } from '../features/integrations/IntegrationsPage';
import { PartnerDetailPage } from '../features/partners/PartnerDetailPage';
import { PartnersListPage } from '../features/partners/PartnersListPage';
import { ReportsPage } from '../features/reports/ReportsPage';
import { TicketsPage } from '../features/tickets/TicketsPage';
import { VenueDetailPage } from '../features/venues/VenueDetailPage';
import { VenuesListPage } from '../features/venues/VenuesListPage';
import { AdminLayout } from '../layouts/AdminLayout';

function ProtectedAdmin() {
  const auth = useOperator();
  if (auth.loading)
    return (
      <div className="full-loading">
        <LoadingState rows={6} />
      </div>
    );
  if (!auth.authenticated) return <Navigate to="/sign-in" replace />;
  return <AdminLayout />;
}

export function App() {
  return (
    <Routes>
      <Route path="/sign-in" element={<SignInPage />} />
      <Route element={<ProtectedAdmin />}>
        <Route index element={<DashboardPage />} />
        <Route path="events" element={<EventsListPage />} />
        <Route path="events/new" element={<CreateEventPage />} />
        <Route path="events/:eventId" element={<EventDetailPage />} />
        <Route path="venues" element={<VenuesListPage />} />
        <Route path="venues/:venueId" element={<VenueDetailPage />} />
        <Route path="partners" element={<PartnersListPage />} />
        <Route path="partners/:partnerId" element={<PartnerDetailPage />} />
        <Route path="tickets" element={<TicketsPage />} />
        <Route path="admissions" element={<AdmissionsPage />} />
        <Route path="reports" element={<ReportsPage />} />
        <Route path="integrations" element={<IntegrationsPage />} />
        <Route path="account" element={<AccountPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
