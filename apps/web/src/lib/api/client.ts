// ky instance with same-origin cookie session.
// 401 → throw special error for auth guard.
import ky from "ky";
import type { AfterResponseState } from "ky";

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export const api = ky.create({
  prefix: "/",
  credentials: "same-origin",
  throwHttpErrors: false,
  hooks: {
    afterResponse: [
      (state: AfterResponseState) => {
        if (state.response.status === 401) {
          throw new ApiError(401, "Unauthorized");
        }
      },
    ],
  },
});
