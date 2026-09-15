import {
  ClockCounterClockwiseIcon,
  GearSixIcon,
  GlobeIcon,
  HardDrivesIcon,
  HouseIcon,
  ShieldCheckIcon,
  SignpostIcon,
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
  { id: "overview", label: "Overview", icon: HouseIcon, index: "00" },
  { id: "certificates", label: "Clusters", icon: HardDrivesIcon, index: "01" },
  { id: "endpoints", label: "Endpoints", icon: GlobeIcon, index: "02" },
  { id: "routes", label: "Routes", icon: SignpostIcon, index: "03" },
  { id: "history", label: "History", icon: ClockCounterClockwiseIcon, index: "04" },
  { id: "ct", label: "CT Check", icon: ShieldCheckIcon, index: "05" },
  { id: "settings", label: "Settings", icon: GearSixIcon, index: "06" },
];

export default function AppSidebar({ activeTab, onTabChange, isLive }) {
  return (
    <Sidebar collapsible="icon" className="border-r border-sidebar-border">
      <SidebarHeader className="px-3 py-4">
        <div className="flex items-center gap-2 group-data-[collapsible=icon]:justify-center">
          <img src="/C.webp" alt="" className="size-6 shrink-0 object-contain" />
          <span className="font-heading text-sm font-semibold tracking-tight group-data-[collapsible=icon]:hidden">
            CertHealthz
          </span>
        </div>
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
                  <span className="ml-auto text-[10px] text-muted-foreground group-data-[collapsible=icon]:hidden">
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
          <span className="text-[11px] text-muted-foreground group-data-[collapsible=icon]:hidden">
            {isLive ? "Connected to live scan" : "Not connected"}
          </span>
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}
