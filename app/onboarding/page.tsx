import type { Metadata } from "next";
import { OnboardingForm } from "./onboarding-form";
import styles from "./onboarding.module.css";
import { OnboardingAuthHeader } from "./onboarding-auth-header";

export const metadata: Metadata = { title: "Set up your workspace — Realverax", description: "Choose the autonomous infrastructure changes your workspace needs to verify." };
export default function OnboardingPage() { return <main className={styles.page}><OnboardingAuthHeader/><section className={`${styles.intro} ${styles.shell}`}><p>SET UP YOUR WORKSPACE</p><h1>What infrastructure changes must you verify?</h1><span>Connect autonomous systems and inspect the resulting state their changes leave behind.</span></section><OnboardingForm/></main>; }
