import { expect, test } from "vite-plus/test";
import { mentionQueryAt, mentionedIds } from "../src/lib/comment-mentions.ts";

test("an @ at the start or after whitespace opens a mention up to the caret", () => {
  expect(mentionQueryAt("@", 1)).toEqual({ start: 0, query: "" });
  expect(mentionQueryAt("ping @nh", 8)).toEqual({ start: 5, query: "nh" });
  expect(mentionQueryAt("ping @nh later", 8)).toEqual({ start: 5, query: "nh" });
  expect(mentionQueryAt("line\n@Bù", 8)).toEqual({ start: 5, query: "Bù" });
});

test("an email address, a finished mention or a caret past a space is not a mention", () => {
  expect(mentionQueryAt("mail nhat@example.com", 21)).toBeNull();
  expect(mentionQueryAt("@Nhat Bui ", 10)).toBeNull();
  expect(mentionQueryAt("no at here", 10)).toBeNull();
});

test("only picks still present in the text are mentioned, once each", () => {
  const picked = [
    { id: "u1", label: "Nhat Bui" },
    { id: "u2", label: "An" },
    { id: "u1", label: "Nhat Bui" },
  ];
  expect(mentionedIds("@Nhat Bui please check", picked)).toEqual(["u1"]);
  expect(mentionedIds("@An and @Nhat Bui", picked)).toEqual(["u1", "u2"]);
  expect(mentionedIds("nobody", picked)).toEqual([]);
});
