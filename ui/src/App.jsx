import { useState } from "react";
import CertificatesPage from "@/components/certificates/CertificatesPage";
import CTCheckPage from "@/components/ct/CTCheckPage";
import HistoryPage from "@/components/history/HistoryPage";
import AppSidebar from "@/components/layout/AppSidebar";
import SettingsPage from "@/components/settings/SettingsPage";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useCerts } from "@/hooks/useCerts";

export default function App() {
  const [activeTab, setActiveTab] = useState("certificates");
  const { certs, isLive, reload: reloadCerts } = useCerts();

  return (
    <TooltipProvider>
      <SidebarProvider defaultOpen={false}>
        <AppSidebar activeTab={activeTab} onTabChange={setActiveTab} isLive={isLive} />

        <SidebarInset className="relative">
          <main className="arch-frame relative mx-auto min-h-svh w-full max-w-336 px-6 py-12 md:px-12">
            {activeTab === "certificates" && (
              <CertificatesPage certs={certs} isLive={isLive} reloadCerts={reloadCerts} />
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
