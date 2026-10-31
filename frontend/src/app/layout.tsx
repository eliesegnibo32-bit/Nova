import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { Providers } from "@/components/providers";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "NOVA — Employé numérique de votre commerce",
  description:
    "NOVA, l'assistant IA WhatsApp pour les petites entreprises de Côte d'Ivoire. Gérez catalogue, stock, commandes, conversations clients et livraisons depuis un tableau de bord unique.",
  keywords: [
    "NOVA",
    "WhatsApp Business",
    "IA",
    "commerce",
    "Côte d'Ivoire",
    "tableau de bord",
    "gestion de stock",
  ],
  authors: [{ name: "NOVA" }],
  applicationName: "NOVA",
  icons: {
    icon: "/logo.svg",
  },
  openGraph: {
    title: "NOVA — Employé numérique de votre commerce",
    description:
      "L'assistant IA WhatsApp pour les petites entreprises de Côte d'Ivoire.",
    siteName: "NOVA",
    type: "website",
    locale: "fr_CI",
  },
};

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#0e8a5f" },
    { media: "(prefers-color-scheme: dark)", color: "#0a1f17" },
  ],
  width: "device-width",
  initialScale: 1,
  maximumScale: 5,
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="fr" suppressHydrationWarning>
      <body
        className={`${geistSans.variable} ${geistMono.variable} antialiased bg-background text-foreground`}
      >
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
