import type { Metadata } from "next";
import { AgentForm } from "./agent-form";

export const metadata: Metadata = {
  title: "Add agent — Evalplane",
  description: "Register an exact infrastructure agent version and test its HTTP connection.",
};

type PageProps = { searchParams: Promise<{ agent?: string }> };

export default async function AddAgentPage({ searchParams }: PageProps) {
	return <AgentForm existingAgentID={(await searchParams).agent || null} />;
}
