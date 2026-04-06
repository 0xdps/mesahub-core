"use client";

import { useRouter } from "next/navigation";

export default function SignOutButton() {
  const router = useRouter();

  async function handleSignOut() {
    await fetch("/api/auth/logout", { method: "POST" });
    router.push("/login");
  }

  return (
    <button
      type="button"
      onClick={handleSignOut}
      className="text-xs text-neutral-400 hover:text-white transition-colors"
    >
      Sign out
    </button>
  );
}
