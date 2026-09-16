import type { Metadata } from "next";
import { Providers } from "../../../providers";
import { TrajectoryView } from "./trajectory-view";

export const metadata: Metadata = { title: "Trial trajectory — Evalplane", description: "Deterministic observable trajectory reconstructed from persisted trial evidence." };

type PageProps = { params: Promise<{ id: string }> };

export default async function TrajectoryPage({ params }: PageProps) {
  const { id } = await params;
  return (
    <Providers>
      <main>
        <TrajectoryView runID={id} />
      </main>
    </Providers>
  );
}
