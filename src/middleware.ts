import { SessionData, sessionOptions } from "@/lib/session";
import { getIronSession } from "iron-session";
import { NextRequest, NextResponse } from "next/server";

const PUBLIC_PREFIXES = ["/login", "/api/health", "/api/auth"];

export async function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;

  const isPublic = PUBLIC_PREFIXES.some((prefix) =>
    pathname.startsWith(prefix)
  );

  if (isPublic) {
    return NextResponse.next();
  }

  // Service-to-service: accept Authorization: Bearer <ADMIN_TOKEN>
  const authHeader = req.headers.get("authorization");
  if (authHeader?.startsWith("Bearer ")) {
    const provided = authHeader.slice(7);
    const adminToken = process.env.ADMIN_TOKEN ?? "";
    if (adminToken && provided === adminToken) {
      return NextResponse.next();
    }
    return NextResponse.json({ error: "Invalid token" }, { status: 401 });
  }

  // Browser: check session cookie
  const res = NextResponse.next();
  const session = await getIronSession<SessionData>(req, res, sessionOptions);

  if (!session.isLoggedIn) {
    // Only redirect GET requests (page navigations) to /login
    // API calls without auth get a 401 JSON response
    if (pathname.startsWith("/api/")) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }
    const loginUrl = new URL("/login", req.url);
    return NextResponse.redirect(loginUrl);
  }

  return res;
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|fonts|favicon.ico|icon).*)"],
};
