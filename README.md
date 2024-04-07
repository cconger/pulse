# The bank of +2

## What is it?
A simple chatbot solution for monitoring twitch chat and keeping track of the +2/-2

### Features

Monitor chat

Any message that contains a +n or -n will be captured.  N is capped at 2
A +2 or -2 can have an optional target.

Target:
 _      => streamer
 @user  => twitch user
 #topic => an abstract concept

Rate limiting on the tuple of (user, target)

---
User queries:

Ask the bot for your balance (how much you've been +d and -d)
Ask the bot for your ledger (how much you've +d vs -d)
  Give a "Alignment" for how much you spend upward and downward



