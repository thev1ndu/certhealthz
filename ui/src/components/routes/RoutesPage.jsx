import NotConnected from "@/components/layout/NotConnected";
import PageHeading from "@/components/layout/PageHeading";
import Section from "@/components/layout/Section";
import CoverageTable from "@/components/routes/CoverageTable";
import RoutesTable from "@/components/routes/RoutesTable";
import { useRoutes } from "@/hooks/useRoutes";

export default function RoutesPage() {
  const { routes, coverage, isLive } = useRoutes();

  return (
    <div>
      <PageHeading
        title="Routes"
        description="Every Ingress and Gateway API route across your clusters, plus a migration coverage report for cutting over from Ingress to Gateway API"
      />

      {!isLive ? (
        <NotConnected />
      ) : (
        <div className="flex flex-col gap-6">
          <Section title="Migration coverage">
            <CoverageTable coverage={coverage} />
          </Section>
          <Section title="Routes">
            <RoutesTable routes={routes} />
          </Section>
        </div>
      )}
    </div>
  );
}
