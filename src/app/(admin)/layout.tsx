import Link from "next/link";
import SignOutButton from "./sign-out-button";

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="h-screen bg-neutral-950 text-neutral-100 flex flex-col">
      <header className="border-b border-neutral-800 px-6 py-3 flex items-center justify-between shrink-0">
        <Link href="/" className="flex items-center gap-2">
          <img src="/logo.svg" alt="sqlite-hub" className="w-6 h-6 rounded" />
          <span className="text-sm font-semibold tracking-tight text-white">sqlite-hub</span>
        </Link>
        <SignOutButton />
      </header>
      <main className="flex-1 flex flex-col overflow-hidden">{children}</main>
    </div>
  );
}
