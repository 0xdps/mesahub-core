import { SessionData, sessionOptions } from "@/lib/session";
import { getIronSession } from "iron-session";
import { NextRequest, NextResponse } from "next/server";

const PUBLIC_PREFIXES = ["/login", "/api/health", "/api/auth"];

// These routes enforce their own per-DB auth (service secret / internal IP)
const DB_SCOPED_PATTERN = /^\/api\/db\/[^/]+(\/exec|\/query)$/;

// Trusted internal header stamped by middleware after session verification.
// Stripped from all incoming requests to prevent external forgery.
export const ADMIN_SESSION_HEADER = "x-sqlite-hub-admin";

export async function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;

  // Strip any externally supplied admin-session header to prevent forgery
  const forwarded = new Headers(req.headers);
  forwarded.delete(ADMIN_SESSION_HEADER);

  const isPublic = PUBLIC_PREFIXES.some((prefix) =>
    pathname.startsWith(prefix)
  );

  if (isPublic) {
    return NextResponse.next({ request: { headers: forwarded } });
  }

  // DB-scoped routes: if session is valid, stamp the trusted header so route
  // handlers know this is an authenticated admin browser request.
  // Skip the session crypto entirely when a Bearer token is present — the
  // route handler will validate it directly.
  if (DB_SCOPED_PATTERN.test(pathname)) {
    if (!forwarded.has("authorization")) {
      const tempRes = NextResponse.next();
      const session = await getIronSession<SessionData>(req, tempRes, sessionOptions);
      if (session.isLoggedIn) {
        forwarded.set(ADMIN_SESSION_HEADER, "1");
      }
    }
    return NextResponse.next({ request: { headers: forwarded } });
  }

  // All other routes: require a valid browser session (session cookie only)

  // Browser: check session cookie
  const res = NextResponse.next({ request: { headers: forwarded } });
  const session = await getIronSession<SessionData>(req, res, sessionOptions);

  if (!session.isLoggedIn) {
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
