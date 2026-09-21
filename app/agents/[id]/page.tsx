import type { Metadata } from "next";
import { AgentDetail } from "./agent-detail";

export const metadata: Metadata = { title: "Agent details — Realverax" };
type PageProps = { params: Promise<{ id: string }> };

export default async function AgentDetailPage({ params }: PageProps) {
  return <AgentDetail agentID={(await params).id} />;
}
