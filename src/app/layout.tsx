import type { Metadata } from "next";

import "./codemirror-override.css";
import "./globals.css";

import { DialogProvider } from "@/components/create-dialog";

export const metadata: Metadata = {
  title: "file-db",
  description: "Centralized SQLite storage service for internal Railway workloads",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body>
        {children}
        <DialogProvider slot="default" />
      </body>
    </html>
  );
}
