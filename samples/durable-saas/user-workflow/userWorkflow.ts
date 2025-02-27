import { WorkflowContext, WorkflowSharedContext, TerminalError } from "@restatedev/restate-sdk";
import { objects, services } from "~restate";
import { User, WriteUserRequest } from "~types/user";

export const run = async (ctx: WorkflowContext, user: User) => {

    console.log("Running workflow for user", user.id)

    ctx.set("stage", "Email Verification");
    const secret = ctx.rand.uuidv4();

    ctx.serviceSendClient<typeof services.Email>(services.Email).sendEmail({
        to: user.email,
        subject: "Verify your email",
        body: `Click the link to verify your email: http://localhost:4000/user/confirm/${ctx.key}/${secret}`
    });

    const clickSecret = await ctx.promise<string>("email-link");
    if (clickSecret !== secret) {
        ctx.set("stage", `Verification failed`);
        console.log("Verification failed");
        throw new TerminalError("Wrong secret from email link");
    }

    user.confirmed = true;
    await ctx.objectClient<typeof objects.User>(objects.User, user.id).write(<WriteUserRequest>user);

    console.log("User verified, wecompleted our workflow :D (this could essentially cover the entire journey of a user")
    ctx.set("stage", "User verified");
    return true;
  };

  export const getStage = async (ctx: WorkflowSharedContext) => ctx.get("stage");

  export const approveEmail = async (ctx: WorkflowSharedContext, secret: string) => ctx.promise<string>("email-link").resolve(secret);