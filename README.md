# openai-discord-bot

A Discord bot that uses OpenAI's GPT-3 API to generate text, generate images and have conversations.

## TODO

- [x] Add a command to generate text from a prompt
- [x] When generating text, print out the original prompt as well as the generated text.
- [x] Add a command to generate an image from a prompt
- [x] Build and run via Docker
- [x] DynamoDB lock. All instances get messages, but get a lock so that only one processes it and responds.
- [x] Infrastructure to deploy the bot to AWS
- [ ] Text completion shouldn't use interactions, so that inputs can be free-form.
- [ ] Add a command to have a conversation with the bot. Will have it in a new thread.
- [ ] If instance fails while holding the lock, another instance will get the lock and respond.
