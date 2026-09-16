import type { Metadata } from "next";
import { Providers } from "../../providers";
import { SuiteTrajectoryView } from "./suite-view";

export const metadata: Metadata = { title: "Suite execution trajectory — Evalplane", description: "Concise per-trial evidence summaries for a suite execution." };

type PageProps = { params: Promise<{ id: string }> };

export default async function SuiteExecutionPage({ params }: PageProps) {
  const { id } = await params;
  return (
    <Providers>
      <main>
        <SuiteTrajectoryView executionID={id} />
      </main>
    </Providers>
  );
}
