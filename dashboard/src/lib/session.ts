import { SessionOptions } from "iron-session";

const sessionSecret = process.env.SESSION_SECRET ?? "";

if (!sessionSecret || sessionSecret.length < 32 || sessionSecret.includes("change-me")) {
  console.error(
    "SESSION_SECRET is not configured correctly. Auth routes will fail until it is set to a strong unique value (≥32 chars)."
  );
}

export interface SessionData {
  isLoggedIn: boolean;
}

export const sessionOptions: SessionOptions = {
  password: sessionSecret,
  cookieName: "sqlitedbhub_session",
  cookieOptions: {
    secure: process.env.NODE_ENV === "production",
    httpOnly: true,
    sameSite: "lax",
  },
};
