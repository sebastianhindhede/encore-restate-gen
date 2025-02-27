# encore-restate-gen
Durable, stateful compute for Encore.ts, in a single command - powered by Restate.

## Getting started

Enabling durability using encore-restate-gen is as simple as running:

```bash
cd <path-to-encore-project>
npx encore-restate-gen
```

or

```bash
npx encore-restate-gen <path-to-encore-project>
```

That's it!

The Restate TypeScript SDK is installed, the toolbox is generated, TypeScript is configured and you are ready to define your durable handlers :D

## Defining durable handlers

You write your Encore services as you always have, but now from those same services, you can also export async Restate handlers.

These handlers can be invoked from anywhere within Encore or even from within other Restate handlers, allowing for very complex patterns, that are easy to reason about and have durable guarantees - even state!

You can define handlers for services, workflows, and virtual objects using the Restate TypeScript SDK, with a small twist - instead of manually defining the Restate services/workflows/virtual object definitions, you just define and export the handlers - the rest is inferred from the Encore service.

A durable service handler may look like this:

```typescript
export const signupUser = async (ctx: Context, req: SignupRequest): Promise<SignupResponse> => {
    ...
}
```

Or a workflow handler like this:

```typescript
export const run = async (ctx: WorkflowContext, user: User) => {
    ...
}
```

Or perhaps even something stateful, like this virtual object handler:

```typescript
export const write = async (ctx: ObjectContext, req: WriteUserRequest): Promise<User> => {
    ...
}
```

If you want a complete, working example, please refer to our [Encore durable saas sample project](https://github.com/sebastianhindhede/encore-restate-gen/tree/main/samples/durable-saas).

For anything else related to Restate, please refer to the [Restate TypeScript documentation](https://docs.restate.dev/get_started/quickstart).

Okay! So you have your example project or you have defined your durable handlers - now, it is time to register them with Restate Server. [You do have Restate Server running, right? If not, just follow the instructions in the quickstart guide and come back here](https://docs.restate.dev/get_started/quickstart).

## Registering your durable handlers with Restate Server

By now, you should have your durable handlers defined in your Encore services and you should have Restate Server running.

Restate Server is the durable execution engine that registers and carries out all invocations of your handlers - it doesn't execute the handlers itself and it doesn't know anything about your Encore project - yet.

After defining your durable handlers in your Encore services, we must register them with Restate Server so it is aware of them and can invoke them.

To do this, you simply run the following command:

```bash
restate deployments register --use-http1.1 <encore-url>/<encore-service-name>
```

*NOTE: Even though Restate supports bidirectional mode via http 2, only http 1.1 is supported for now. This is because Restate calls into the Encore API via auto-generated raw endpoints to run the code, whenever a handler is invoked.*

## Calling the handlers

### From within Encore, outside of Restate context

To call a Restate service/virtual object handler or workflow outside of the Restate context, you use the clients. This is often done from an Encore API endpoint and can be seen as the entrypoint to durability land.

You have the request-response clients: ``serviceClient``, ``objectClient`` and ``workflowClient``.

You also have the fire-and-forget clients: ``serviceSendClient``, ``objectSendClient`` and ``workflowSendClient``.

**Request-response example:**

```typescript
import { services, serviceClient } from "~restate";
const submission = await serviceClient(services.UserManager).signupUser(req);
```

**Fire-and-forget example:**

```typescript
import { workflows, workflowSendClient } from "~restate";
workflowSendClient(workflows.User, workflowId).run(user);
```

### From within other Restate handlers, using the Restate context

Oftentimes, we are already in durability land and have to call out to other durable handlers, workflows or virtual objects.

To call another Restate service/virtual object handler or workflow, we use the Restate context.

**Request-response example:**

```typescript
import { Context } from "@restatedev/restate-sdk";
import { objects, workflows } from "~restate";
import { WriteUserRequest, SignupRequest, SignupResponse } from "~types/user";

export const signupUser = async (ctx: Context, req: SignupRequest): Promise<SignupResponse> => { 
    const user = await ctx.objectClient<typeof objects.User>(objects.User, ctx.rand.uuidv4()).write(<WriteUserRequest>{
        ...req,
        confirmed: false,
    });

    ...
};
```

**Fire-and-forget example:**

```typescript
    import { WorkflowContext } from "@restatedev/restate-sdk";
    import { services } from "~restate";
    import { User } from "~types/user";

    export const run = async (ctx: WorkflowContext, user: User) => {

        console.log("Running workflow for user", user.id)

        ctx.set("stage", "Email Verification");
        const secret = ctx.rand.uuidv4();

        ctx.serviceSendClient<typeof services.Email>(services.Email).sendEmail({
            to: user.email,
            subject: "Verify your email",
            body: `Click the link to verify your email: http://localhost:4000/user/confirm/${ctx.key}/${secret}`
        });

        ...
    }
```

## Fully typed auto-complete

As each durable service, workflow or virtual object get built out by encore-restate-gen, it also gets added to a local registry, giving you all of the auto-complete goodness you desire.

Anywhere in your codebase, you simply import what you need like

```typescript
import { services, workflows, objects } from "~restate";
```

and you are ready to go:

![](https://m90m2siljc.ufs.sh/f/75fsEUGECjTZ5P1rQJylGuQR0UpymCOqgnxFY4KcdArB1oEi)

![](https://m90m2siljc.ufs.sh/f/75fsEUGECjTZKQ3sRYJ6CktVs3l1f8mOKgaEJ2qNeDdv5GQp)

## It is that simple!

Seriously. If you followed these simple steps, you now have durable, stateful compute in your Encore project, powered by Restate.

Fully typed and auto-complete ready.

Without worrying about Postgres, without worrying about managers or workers.

As it should be.

![wow](https://m90m2siljc.ufs.sh/f/75fsEUGECjTZ5440rdMylGuQR0UpymCOqgnxFY4KcdArB1oE)

What will you build with it!? **I cannot wait to see!**

## Configuration options

When you run your project using `encore run`, you can pass the `RESTATE_SERVER_URL` environment variable to point to your Restate server.

```bash
RESTATE_SERVER_URL=<my-restate-server-url> encore run
```

If not supplied, it will default to the Restate server running on http://localhost:8080. When deploying your project, make sure this environment variable is properly configured, otherwise it will not work.

## How encore-restate-gen works and a bit of background

encore-restate-gen is a community created and maintained CLI tool, that you run in a terminal.

As it runs, it watches your Encore project and automatically detects exported async handlers for Restate services, workflows, and virtual objects.

Whenever it detects a change, it generates the code needed to seamlessly discover and call these handlers in Encore via Restate, effectively bringing durable, stateful compute to your Encore project in a very smooth developer experience.

Encore becomes both the service catalog, API gateway and the workhorse running the business logic, with Restate Server being the durable execution engine, which keeps track of invocations, and makes sure everything runs as expected, with powerful durability guarantees and state. encore-restate-gen is the glue that binds the two together.

It will:

- Detect the package manager you are using.
- Install the necessary Restate TypeScript SDK modules.
- Auto-configre your tsconfig.json with the necesary paths and includes.
- Continously scan and monitor your Encore services for exported Restate handlers.
- Build out the Restate services/workflows/virtual objects based on the handlers and the Encore service name.
- Generate routing, adapter, service discovery and invocation code to seamlessly call back and forth between Restate and Encore.

This project was initially created and maintained by me, Sebastian, and it sprung out of a need for durable, stateful compute in Encore.ts, which already on its own delivered a joyful developer experience - but I needed a similarly joyful way to add durability, without imposing the complexity of some frameworks and I didn't want yet another Postgres wrapper.

Restate, with its single binary "system-on-a-chip"-like design, its powerful performance and its TypeScript SDK made it an obvious choice.

The last piece of the puzzle was bringing these two systems together, for a seamless developer experience - and with encore-restate-gen, I feel like we have taken a big step in the right direction.

## Sample project

Check out our [sample project](https://github.com/sebastianhindhede/encore-restate-gen/tree/main/samples/durable-saas) to see a working example of how this looks and the [Restate documentation](https://docs.restate.dev/get_started/quickstart) for everything else related to the Restate TypeScript SDK.

## Troubleshooting

Make sure you are running a Restate server or cluster that is reachable on either http://localhost:8080 or the url you have set the RESTATE_SERVER_URL environment variable to.

## Roadmap

- [ ] Support for Encore tracing.
- [ ] HTTP/2 support.
... Any other suggestions?

## Closing notes

This is a very early version and it only supports TypeScript.

Despite that, it has quickly proven itself a very useful and delightful tool in my own toolbox, and I am very excited to share it with everyone.

Any and all feedback, questions, suggestions and contributions are welcome!

Find me on Discord [@sebastianhindhede](https://discord.com/users/sebastianhindhede) or start a discussion on [github](https://github.com/sebastianhindhede/encore-restate-gen/discussions).

## License

This tool is licensed under the MIT license. See the LICENSE file for details. 

## Contributing

Feel free to contribute to the project by opening an issue or a PR.

## Acknowledgements

- [Restate](https://restate.dev) for providing the durable, stateful compute platform. [Check out their Discord!](https://discord.gg/skW3AZ6uGd)
- [Encore](https://encore.dev) for providing the Encore framework. [Check out their Discord!](https://encore.dev/discord)