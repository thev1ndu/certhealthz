import { useState } from "react";
import CertificatesPage from "@/components/certificates/CertificatesPage";
import CTCheckPage from "@/components/ct/CTCheckPage";
import HistoryPage from "@/components/history/HistoryPage";
import AppSidebar from "@/components/layout/AppSidebar";
import OverviewPage from "@/components/overview/OverviewPage";
import SettingsPage from "@/components/settings/SettingsPage";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useCerts } from "@/hooks/useCerts";

export default function App() {
  const [activeTab, setActiveTab] = useState("overview");
  const [pendingCertId, setPendingCertId] = useState(null);
  const { certs, isLive, reload: reloadCerts } = useCerts();

  function navigate(tab, certId = null) {
    setPendingCertId(certId);
    setActiveTab(tab);
  }

  return (
    <TooltipProvider>
      <SidebarProvider defaultOpen={false}>
        <AppSidebar activeTab={activeTab} onTabChange={navigate} isLive={isLive} />

        <SidebarInset className="relative">
          <main className="arch-frame relative mx-auto min-h-svh w-full max-w-336 px-6 py-12 md:px-12">
            {activeTab === "overview" && (
              <OverviewPage certs={certs} isLive={isLive} onNavigate={navigate} />
            )}
            {activeTab === "certificates" && (
              <CertificatesPage
                certs={certs}
                isLive={isLive}
                reloadCerts={reloadCerts}
                initialCertId={pendingCertId}
              />
            )}
            {activeTab === "history" && (
              <HistoryPage isLive={isLive} active={activeTab === "history"} />
            )}
            {activeTab === "ct" && <CTCheckPage isLive={isLive} />}
            {activeTab === "settings" && (
              <SettingsPage isLive={isLive} onSaved={reloadCerts} />
            )}
          </main>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  );
}
