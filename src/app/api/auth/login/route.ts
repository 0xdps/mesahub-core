import { SessionData, sessionOptions } from "@/lib/session";
import { timingSafeEqual } from "crypto";
import { getIronSession } from "iron-session";
import { cookies } from "next/headers";
import { NextResponse } from "next/server";

export async function POST(req: Request) {
  const body = await req.json().catch(() => null);
  if (!body || typeof body.token !== "string") {
    return NextResponse.json({ error: "token is required" }, { status: 400 });
  }

  const adminToken = process.env.ADMIN_TOKEN ?? "";
  if (!adminToken) {
    return NextResponse.json({ error: "ADMIN_TOKEN not configured" }, { status: 500 });
  }

  // Timing-safe comparison
  const provided = Buffer.from(body.token.padEnd(adminToken.length));
  const expected = Buffer.from(adminToken.padEnd(body.token.length));
  const match =
    provided.length === expected.length &&
    timingSafeEqual(provided, expected);

  if (!match) {
    return NextResponse.json({ error: "Invalid token" }, { status: 401 });
  }

  const cookieStore = await cookies();
  const res = NextResponse.json({ success: true });
  const session = await getIronSession<SessionData>(cookieStore, sessionOptions);
  session.isLoggedIn = true;
  await session.save();

  return res;
}
