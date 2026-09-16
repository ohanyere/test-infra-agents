import type { Metadata } from "next";
import { OnboardingForm } from "./onboarding-form";
import styles from "./onboarding.module.css";
import { OnboardingAuthHeader } from "./onboarding-auth-header";

export const metadata: Metadata = { title: "Set up your workspace — Evalplane", description: "Choose the infrastructure-agent workflows and scenarios relevant to your workspace." };
export default function OnboardingPage() { return <main className={styles.page}><OnboardingAuthHeader/><section className={`${styles.intro} ${styles.shell}`}><p>SET UP YOUR WORKSPACE</p><h1>Tell us what you want to evaluate.</h1><span>Create your workspace and choose the infrastructure areas and agent workflows you care about first.</span></section><OnboardingForm/></main>; }
