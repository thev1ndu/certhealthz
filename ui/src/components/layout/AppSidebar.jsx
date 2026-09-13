import {
  CertificateIcon,
  ClockCounterClockwiseIcon,
  GearSixIcon,
  ShieldCheckIcon,
} from "@phosphor-icons/react";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";

export const TABS = [
  { id: "certificates", label: "Certificates", icon: CertificateIcon, index: "01" },
  { id: "history", label: "History", icon: ClockCounterClockwiseIcon, index: "02" },
  { id: "ct", label: "CT Check", icon: ShieldCheckIcon, index: "03" },
  { id: "settings", label: "Settings", icon: GearSixIcon, index: "04" },
];

export default function AppSidebar({ activeTab, onTabChange, isLive }) {
  return (
    <Sidebar collapsible="icon" className="border-r border-sidebar-border">
      <SidebarHeader className="px-3 py-4">
        <span className="font-heading text-sm font-semibold tracking-tight group-data-[collapsible=icon]:hidden">
          CertHealthz
        </span>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarMenu>
            {TABS.map((tab) => (
              <SidebarMenuItem key={tab.id}>
                <SidebarMenuButton
                  isActive={activeTab === tab.id}
                  tooltip={tab.label}
                  onClick={() => onTabChange(tab.id)}
                >
                  <tab.icon size={16} />
                  <span>{tab.label}</span>
                  <span className="ml-auto font-mono text-[10px] text-muted-foreground group-data-[collapsible=icon]:hidden">
                    {tab.index}
                  </span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            ))}
          </SidebarMenu>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter className="px-3 pb-4">
        <div className="flex items-center gap-2 px-2.5 py-2 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
          <span
            className={
              "size-1.5 shrink-0 rounded-full " + (isLive ? "bg-status-ok" : "bg-status-neutral")
            }
          />
          <span className="font-mono text-[11px] text-muted-foreground group-data-[collapsible=icon]:hidden">
            {isLive ? "Connected to live scan" : "Not connected"}
          </span>
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}
