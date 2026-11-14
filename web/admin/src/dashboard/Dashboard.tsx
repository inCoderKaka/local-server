import { HostUse } from './HostUse';
import { QuietHoursForm } from './QuietHoursForm';
import { SharingCard } from './SharingCard';
import { StatusCards } from './StatusCards';
import { UsageChart } from './UsageChart';

/** The dashboard: host use now and over time, sharing with quiet hours, engine and tailnet. */
export default function Dashboard() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Dashboard</h1>
      <HostUse />
      <UsageChart />
      <div className="grid gap-4 lg:grid-cols-2">
        <SharingCard />
        <QuietHoursForm />
      </div>
      <StatusCards />
    </div>
  );
}
