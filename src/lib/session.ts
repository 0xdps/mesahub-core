import { SessionOptions } from "iron-session";

const sessionSecret = process.env.SESSION_SECRET ?? "";

if (process.env.NODE_ENV === "production") {
  if (!sessionSecret || sessionSecret.length < 32 || sessionSecret.includes("change-me")) {
    throw new Error("SESSION_SECRET must be set to a strong value (>=32 chars) in production");
  }
}

export interface SessionData {
  isLoggedIn: boolean;
}

export const sessionOptions: SessionOptions = {
  password: sessionSecret || "change-me-in-production-min-32-chars!!",
  cookieName: "sqlitedbhub_session",
  cookieOptions: {
    secure: process.env.NODE_ENV === "production",
    httpOnly: true,
    sameSite: "lax",
  },
};
