import type { Metadata } from "next";
import localFont from "next/font/local";
import "./globals.css";
import "./hero-scale.css";
import "./verification-proof.css";
import { Providers } from "./providers";

const geistSans = localFont({
  src: "../node_modules/next/dist/next-devtools/server/font/geist-latin.woff2",
  variable: "--font-geist-sans",
  display: "swap",
});

const geistMono = localFont({
  src: "../node_modules/next/dist/next-devtools/server/font/geist-mono-latin.woff2",
  variable: "--font-geist-mono",
  display: "swap",
});

export const metadata: Metadata = {
  title: "Realverax — Independent infrastructure agent verification",
  description:
    "Inspect what infrastructure agents change and independently verify outcomes, side effects, stability, and evidence-backed forward risk.",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body className={`${geistSans.variable} ${geistMono.variable}`}><style>{`a[class*="button"],button[class*="button"],a[class*="primaryAction"],button[class*="primaryAction"]{border-color:#171717!important;background:#171717!important;color:#fff!important}`}</style><Providers>{children}</Providers></body>
    </html>
  );
}
