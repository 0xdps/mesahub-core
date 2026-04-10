import { SessionOptions } from "iron-session";

const sessionSecret = process.env.SESSION_SECRET ?? "";

// Skip validation during `next build` — Railway env vars are only injected at
// runtime, not during the Docker image build phase.
if (process.env.NEXT_PHASE !== "phase-production-build") {
  if (
    !sessionSecret ||
    sessionSecret.length < 32 ||
    sessionSecret.includes("change-me")
  ) {
    throw new Error(
      "SESSION_SECRET must be set to a strong, unique value (≥32 chars) and must not contain 'change-me'"
    );
  }
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
