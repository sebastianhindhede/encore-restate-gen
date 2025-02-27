import { Context } from "@restatedev/restate-sdk";

export const sendEmail = async (ctx: Context, email: { to: string, subject: string, body: string }) => {
    await ctx.run(() => console.log('Sending email with subject', email.subject, 'to', email.to, ':', email.body))
    await ctx.sleep(3_000);
    await ctx.run(() => console.log('Email sent!'))
    return true;
};